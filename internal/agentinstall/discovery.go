package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/agentjoin"
)

// 自动发现式入网的默认等待节奏：十分钟足够人工审批，之后可重跑同一命令继续等待。
const (
	defaultGrantPollTimeout  = 10 * time.Minute
	defaultGrantPollInterval = 5 * time.Second
	grantStateVersion        = 1
	grantStateFile           = "discovery.json"
)

// GrantOptions 是自动发现式入网所需的输入；授权秘密只经 stdin 或私有文件传入。
type GrantOptions struct {
	ServerURL    string
	GrantSecret  string
	Name         string
	Host         string
	SSHPort      int
	AllowHTTP    bool
	PollTimeout  time.Duration
	PollInterval time.Duration
}

// grantState 持久化可恢复的入网进度，不保存授权秘密明文。
type grantState struct {
	Version      int         `json:"version"`
	ServerURL    string      `json:"server_url"`
	GrantPrefix  string      `json:"grant_prefix"`
	Origin       string      `json:"origin,omitempty"`
	RequestID    string      `json:"request_id,omitempty"`
	RequestToken string      `json:"request_token,omitempty"`
	NodeID       string      `json:"node_id,omitempty"`
	NodeName     string      `json:"node_name,omitempty"`
	Result       *grantClaim `json:"result,omitempty"`
}

// Enroll 执行一次自动发现式入网：协商配置、领取节点凭据并交给 systemd 托管。
func (installer *Installer) Enroll(ctx context.Context, options GrantOptions) (Summary, error) {
	options, err := installer.normalizeGrantOptions(options)
	if err != nil {
		return Summary{}, err
	}
	release, err := installer.begin(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = release() }()
	identity, err := installer.loadOrCreateIdentity()
	if err != nil {
		return Summary{}, err
	}
	state, err := installer.loadGrantState(options)
	if err != nil {
		return Summary{}, err
	}
	client := installer.newDiscoveryClient(options.ServerURL, options.GrantSecret, identity)
	client.origin = state.Origin
	if state.Result == nil {
		if err := installer.runGrantProtocol(ctx, client, state, options); err != nil {
			return Summary{}, err
		}
	}
	if err := installer.completeGrantInstall(ctx, *state.Result); err != nil {
		return Summary{}, fmt.Errorf("node enrollment is saved; rerun this command to finish installing: %w", err)
	}
	return Summary{NodeID: state.Result.NodeID, ServerURL: options.ServerURL, ConfigPath: installer.paths.config}, nil
}

func (installer *Installer) normalizeGrantOptions(options GrantOptions) (GrantOptions, error) {
	server, err := agentjoin.NormalizeServerURL(options.ServerURL, options.AllowHTTP)
	if err != nil {
		return GrantOptions{}, err
	}
	secret, err := agentenrollment.DescribeSecret(agentenrollment.GrantSecretPrefix, strings.TrimSpace(options.GrantSecret))
	if err != nil {
		return GrantOptions{}, errors.New("grant secret is not a Beat enrollment secret; pass it through stdin or a 0600 file")
	}
	options.ServerURL, options.GrantSecret = server, secret.Plaintext
	if options.Name == "" {
		if options.Name, err = installer.hostname(); err != nil {
			return GrantOptions{}, fmt.Errorf("detect hostname: %w", err)
		}
	}
	options.Name, options.Host = strings.TrimSpace(options.Name), strings.TrimSpace(options.Host)
	if options.SSHPort == 0 {
		if options.SSHPort, err = installer.sshPort(); err != nil {
			return GrantOptions{}, fmt.Errorf("detect SSH port: %w", err)
		}
	}
	if options.PollTimeout <= 0 {
		options.PollTimeout = defaultGrantPollTimeout
	}
	if options.PollInterval <= 0 {
		options.PollInterval = defaultGrantPollInterval
	}
	return options, nil
}

// loadGrantState 读取本地进度；已有记录必须属于同一服务端与同一条授权。
func (installer *Installer) loadGrantState(options GrantOptions) (*grantState, error) {
	var state grantState
	found, err := loadPrivateJSON(filepath.Join(installer.paths.stateDir, grantStateFile), &state)
	if err != nil {
		return nil, err
	}
	if !found {
		secret, _ := agentenrollment.DescribeSecret(agentenrollment.GrantSecretPrefix, options.GrantSecret)
		return &grantState{Version: grantStateVersion, ServerURL: options.ServerURL,
			GrantPrefix: secret.Prefix}, nil
	}
	if state.Version != grantStateVersion || state.ServerURL != options.ServerURL {
		return nil, errors.New("another enrollment is recorded; rerun the original command to recover")
	}
	secret, _ := agentenrollment.DescribeSecret(agentenrollment.GrantSecretPrefix, options.GrantSecret)
	if state.GrantPrefix != secret.Prefix {
		return nil, errors.New("another enrollment is recorded; rerun the original command to recover")
	}
	return &state, nil
}

func (installer *Installer) saveGrantState(state *grantState) error {
	return installer.saveJSON(filepath.Join(installer.paths.stateDir, grantStateFile), state)
}

// runGrantProtocol 走到领取节点凭据为止，期间每一步都可从中断处恢复。
func (installer *Installer) runGrantProtocol(ctx context.Context, client *discoveryClient,
	state *grantState, options GrantOptions) error {
	metadata := discoveryMetadata(options)
	if state.RequestID != "" && state.Origin == "" {
		return errors.New("stored enrollment is missing its server origin; remove it before enrolling again")
	}
	if state.RequestID == "" {
		challenge, err := client.challenge(ctx, metadata)
		if err != nil {
			return err
		}
		credential, err := client.createRequest(ctx, challenge, metadata)
		if err != nil {
			return err
		}
		state.RequestID, state.RequestToken = credential.RequestID, credential.RequestToken
		state.Origin = challenge.Origin
		if err := installer.saveGrantState(state); err != nil {
			return fmt.Errorf("save enrollment progress; rerun this command to recover: %w", err)
		}
	}
	approved, err := installer.awaitGrantApproval(ctx, client, state, options)
	if err != nil {
		return err
	}
	claimed, err := client.claim(ctx, state.RequestID, state.RequestToken, approved.NodeID, approved.Ticket)
	if err != nil {
		return err
	}
	state.Result, state.NodeID, state.NodeName = &claimed, claimed.NodeID, claimed.NodeName
	if err := installer.saveGrantState(state); err != nil {
		return fmt.Errorf("save enrollment progress; rerun this command to recover: %w", err)
	}
	return nil
}

// awaitGrantApproval 轮询到审批通过为止；拒绝、过期与超时都会给出稳定提示。
func (installer *Installer) awaitGrantApproval(ctx context.Context, client *discoveryClient,
	state *grantState, options GrantOptions) (grantStatus, error) {
	deadline := time.Now().Add(options.PollTimeout)
	for {
		status, err := client.status(ctx, state.RequestID, state.RequestToken)
		if err != nil {
			return grantStatus{}, err
		}
		switch agentenrollment.RequestStatus(status.Status) {
		case agentenrollment.RequestApproved:
			if status.Ticket == "" || status.NodeID == "" {
				return grantStatus{}, errors.New("server approved the request without a claim ticket")
			}
			return status, nil
		case agentenrollment.RequestRejected:
			return grantStatus{}, fmt.Errorf("enrollment request was rejected by the administrator (reason: %s)",
				status.Reason)
		case agentenrollment.RequestExpired:
			return grantStatus{}, errors.New("enrollment request expired; run the command again with a new grant command")
		case agentenrollment.RequestClaimed:
			return grantStatus{}, errors.New("this node already claimed its credential; remove the stored enrollment to start over")
		}
		if time.Now().After(deadline) {
			return grantStatus{}, errors.New("enrollment is still waiting for approval; approve it in Beat and rerun the same command")
		}
		if err := waitGrantPoll(ctx, options.PollInterval); err != nil {
			return grantStatus{}, err
		}
	}
}

func waitGrantPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// discoveryMetadata 汇总 Agent 自报信息：全部是展示建议，不参与授权判断。
func discoveryMetadata(options GrantOptions) agentenrollment.Metadata {
	return agentenrollment.Metadata{Hostname: options.Name, AdvertisedHost: options.Host,
		OS: installerOS(), Architecture: installerArchitecture(), AgentVersion: "",
		Virtualization: "", SSHPort: options.SSHPort, InstallationNonce: installNonce()}
}

// completeGrantInstall 写入配置并启动 systemd 服务，写配置前先确认没有冲突。
func (installer *Installer) completeGrantInstall(ctx context.Context, claim grantClaim) error {
	config := grantConfig(claim)
	if err := installer.checkGrantConfig(config); err != nil {
		return err
	}
	unit := unitFile(installer.paths.binary, installer.paths.config)
	if err := installer.checkSystemd(ctx, unit); err != nil {
		return err
	}
	plan, err := installer.planBinary(installer.paths.config)
	if err != nil {
		return err
	}
	if err := installer.installBinary(plan); err != nil {
		return err
	}
	if err := installer.saveJSON(installer.paths.config, config); err != nil {
		return err
	}
	return installer.startService(ctx, unit)
}

func grantConfig(claim grantClaim) agentjoin.Config {
	interval := claim.ReportInterval
	if interval == "" {
		interval = agentjoin.DefaultReportInterval
	}
	return agentjoin.Config{ServerURL: claim.ServerURL, AgentToken: claim.AgentToken,
		NodeName: claim.NodeName, AdvertisedHost: claim.Host, SSHPort: claim.Port, ReportInterval: interval}
}

// checkGrantConfig 复用同一份配置，或在配置不存在时写入；不一致时拒绝覆盖。
func (installer *Installer) checkGrantConfig(config agentjoin.Config) error {
	existing, err := readPrivate(installer.paths.config)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var current agentjoin.Config
	if err := decodeJSON(strings.NewReader(string(existing)), &current); err != nil {
		return err
	}
	if current != config {
		return errors.New("existing agent config differs from enrollment; it will not be overwritten")
	}
	return nil
}
