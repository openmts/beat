package agentinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/model"
)

type UpdateOptions struct {
	ConfigPath   string
	ManifestPath string
	TrustPath    string
	ArtifactID   string
	NodeID       string
}

func (installer *Installer) EnableUpdates(ctx context.Context, options UpdateOptions) (resultErr error) {
	if options.ConfigPath == "" {
		options.ConfigPath = installer.paths.config
	}
	release, err := installer.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	config, err := agent.LoadConfig(options.ConfigPath)
	if err != nil {
		return err
	}
	manager, err := installer.UpdateManager(ctx, options.ConfigPath)
	if err != nil {
		return err
	}
	nodeID, err := installer.updateNodeID(config, options.NodeID)
	if err != nil {
		return err
	}
	settings := agentupdate.Settings{Enabled: true, NodeID: nodeID}
	if err = agentupdate.ValidateSettings(settings, config.ServerURL); err != nil {
		return err
	}
	bootstrap, err := installer.updateBootstrap(config, options)
	if err != nil {
		return err
	}
	bootstrap.Config.NodeID = nodeID
	bootstrap.Config.Manager = manager
	if err = installer.matchRunningBootstrap(bootstrap); err != nil {
		return err
	}
	return installer.installVerifiedUpdates(ctx, bootstrap, options.ConfigPath)
}

func (installer *Installer) installVerifiedUpdates(ctx context.Context, bootstrap agentupdate.BootstrapOptions, configPath string) error {
	var err error
	if bootstrap.Config.Installed, err = agentupdate.Bootstrap(ctx, bootstrap); err != nil {
		return err
	}
	if err = agentupdate.StoreTrust(installer.paths.binary, bootstrap.Config.Keys); err != nil {
		return err
	}
	if err = agentupdate.InstallHelper(ctx, bootstrap.Config); err != nil {
		return err
	}
	if err = installer.startUpdateHelper(ctx, configPath); err != nil {
		return err
	}
	if err = installer.enableUpdateSettings(configPath, agentupdate.Settings{Enabled: true, NodeID: bootstrap.Config.NodeID}); err != nil {
		return err
	}
	_, err = installer.systemctl(ctx, "restart", ServiceName)
	return err
}

func (installer *Installer) updateBootstrap(config agent.Config, options UpdateOptions) (agentupdate.BootstrapOptions, error) {
	manifest, err := readPrivate(options.ManifestPath)
	if err != nil {
		return agentupdate.BootstrapOptions{}, err
	}
	trusted, err := readPrivate(options.TrustPath)
	if err != nil {
		return agentupdate.BootstrapOptions{}, err
	}
	var signed protocol.SignedManifest
	var keys []protocol.SigningTrust
	if err = decodeJSON(bytes.NewReader(manifest), &signed); err != nil {
		return agentupdate.BootstrapOptions{}, err
	}
	if err = decodeJSON(bytes.NewReader(trusted), &keys); err != nil {
		return agentupdate.BootstrapOptions{}, err
	}
	return agentupdate.BootstrapOptions{ArtifactID: options.ArtifactID, Manifest: signed, Config: agentupdate.Config{Enabled: true,
		Executable: installer.paths.binary, ServerURL: config.ServerURL, Keys: keys, OS: installer.goos, Arch: runtime.GOARCH,
		AgentProtocol: model.AgentReportProtocol, Now: func() time.Time { return time.Now().UTC() }}}, nil
}

func (installer *Installer) updateNodeID(config agent.Config, explicit string) (string, error) {
	var state enrollment
	found, err := loadPrivateJSON(filepath.Join(installer.paths.stateDir, "enrollment.json"), &state)
	if err != nil {
		return "", err
	}
	if !found {
		if !protocol.ValidSigningKeyID(explicit) {
			return "", errors.New("manual installation requires --node-id")
		}
		return explicit, nil
	}
	if err = validateResult(state); err != nil {
		return "", err
	}
	if state.Result.Config.ServerURL != config.ServerURL || state.Result.Config.AgentToken != config.AgentToken {
		return "", errors.New("enrollment does not match current identity")
	}
	if explicit != "" && explicit != state.Result.NodeID {
		return "", errors.New("node-id conflicts with enrolled identity")
	}
	return state.Result.NodeID, nil
}

func (installer *Installer) enableUpdateSettings(path string, settings agentupdate.Settings) error {
	data, err := readPrivate(path)
	if err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if err = json.Unmarshal(data, &config); err != nil {
		return err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	config["updates"] = encoded
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return installer.write(path, append(data, '\n'), privateFile)
}

// 当前CLI必须就是待启用的已签名安装镜像，避免给不支持helper的旧Agent启用新配置。
func (installer *Installer) matchRunningBootstrap(options agentupdate.BootstrapOptions) error {
	manifest, err := protocol.DecodeManifest(options.Manifest.Payload)
	if err != nil {
		return err
	}
	if !installer.matchesRunningVersion(manifest.Version) {
		return agentupdate.ErrVersion
	}
	source, err := installer.executable()
	if err != nil {
		return err
	}
	data, err := readSourceExecutable(source)
	if err != nil {
		return err
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.ID == options.ArtifactID && artifact.Size == int64(len(data)) && artifact.SHA256 == binaryDigest(data) {
			return nil
		}
	}
	return agentupdate.ErrManualOnly
}

func (installer *Installer) matchesRunningVersion(version string) bool {
	return installer.version != nil && protocol.SameReleaseVersion(installer.version(), version)
}
