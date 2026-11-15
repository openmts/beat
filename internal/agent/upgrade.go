package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	beatv1 "beat/internal/protocol/beatv1"
	"beat/internal/version"
)

var ErrUpgradeRestart = errors.New("agent restart requested after upgrade")

const (
	maxAgentUpgradeSize = 100 << 20
	minUpgradeTimeout   = 30 * time.Second
	maxUpgradeTimeout   = 10 * time.Minute
)

func (r *Runtime) handleUpgrade(ctx context.Context, cancel context.CancelFunc, upgrade *beatv1.AgentUpgrade) {
	r.mu.Lock()
	if r.upgrading {
		r.mu.Unlock()
		r.emitUpgradeEvent(ctx, upgrade.GetOperationId(), "failed", "已有 Agent 升级任务正在执行")
		return
	}
	r.upgrading = true
	r.mu.Unlock()

	fail := func(message string) {
		r.cfg.Logger.Warn("agent_upgrade_failed", "operation_id", upgrade.GetOperationId(), "error", message)
		r.emitUpgradeEvent(ctx, upgrade.GetOperationId(), "failed", message)
		r.mu.Lock()
		r.upgrading = false
		r.mu.Unlock()
	}

	if err := validateUpgradeInstruction(r.cfg.ServerURL, upgrade); err != nil {
		fail(err.Error())
		return
	}
	executable, err := os.Executable()
	if err != nil {
		fail("读取 Agent 安装路径失败: " + err.Error())
		return
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		fail("解析 Agent 安装路径失败: " + err.Error())
		return
	}
	if !allowedUpgradeTarget(executable) {
		fail("当前 Agent 不在标准安装路径，拒绝自更新")
		return
	}
	r.emitUpgradeEvent(ctx, upgrade.GetOperationId(), "installing", "正在下载并校验 Agent 安装包")

	timeout := time.Duration(upgrade.GetTimeoutSeconds()) * time.Second
	if timeout < minUpgradeTimeout || timeout > maxUpgradeTimeout {
		timeout = 5 * time.Minute
	}
	downloadCtx, downloadCancel := context.WithTimeout(ctx, timeout)
	defer downloadCancel()
	stagedPath, err := r.downloadUpgrade(downloadCtx, upgrade, filepath.Dir(executable))
	if err != nil {
		fail(err.Error())
		return
	}
	defer func() { _ = os.Remove(stagedPath) }()

	versionCtx, versionCancel := context.WithTimeout(ctx, 15*time.Second)
	output, err := exec.CommandContext(versionCtx, stagedPath, "--version").CombinedOutput()
	versionCancel()
	if err != nil {
		fail("新版本自检失败: " + strings.TrimSpace(string(output)))
		return
	}
	if !strings.Contains(string(output), upgrade.GetTargetVersion()) {
		fail("安装包版本与目标版本不一致")
		return
	}
	if err := replaceAgentBinary(executable, stagedPath); err != nil {
		fail("安装 Agent 失败: " + err.Error())
		return
	}
	r.cfg.Logger.Info("agent_upgrade_installed", "operation_id", upgrade.GetOperationId(), "from_version", version.Version, "to_version", upgrade.GetTargetVersion(), "path", executable)
	r.emitUpgradeEvent(ctx, upgrade.GetOperationId(), "restarting", "新版本已安装，正在重启 Agent")
	r.mu.Lock()
	r.restart = true
	r.mu.Unlock()
	time.Sleep(250 * time.Millisecond)
	cancel()
}

func validateUpgradeInstruction(serverURL string, upgrade *beatv1.AgentUpgrade) error {
	if upgrade == nil || strings.TrimSpace(upgrade.GetOperationId()) == "" {
		return errors.New("升级指令缺少操作 ID")
	}
	target := strings.TrimSpace(upgrade.GetTargetVersion())
	if target == "" || version.Compare(target, version.Version) <= 0 {
		return errors.New("升级目标版本必须高于当前版本")
	}
	if upgrade.GetArch() != runtime.GOARCH {
		return fmt.Errorf("升级架构不匹配: 指令为 %s，当前为 %s", upgrade.GetArch(), runtime.GOARCH)
	}
	if upgrade.GetSize() <= 0 || upgrade.GetSize() > maxAgentUpgradeSize {
		return errors.New("升级包大小无效")
	}
	digest := strings.ToLower(strings.TrimSpace(upgrade.GetSha256()))
	if len(digest) != sha256.Size*2 {
		return errors.New("升级包 SHA-256 无效")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return errors.New("升级包 SHA-256 无效")
	}
	downloadURL, err := url.Parse(strings.TrimSpace(upgrade.GetDownloadUrl()))
	if err != nil {
		return errors.New("升级包地址无效")
	}
	server, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || server.Host == "" {
		return errors.New("Agent 服务端地址无效")
	}
	if !downloadURL.IsAbs() {
		if !strings.HasPrefix(downloadURL.Path, "/downloads/beat-agent/") {
			return errors.New("升级包地址不在允许范围内")
		}
		downloadURL = server.ResolveReference(downloadURL)
	}
	if !strings.EqualFold(downloadURL.Scheme, server.Scheme) || !strings.EqualFold(downloadURL.Host, server.Host) {
		return errors.New("升级包地址必须与当前 Beat Server 同源")
	}
	if !strings.HasPrefix(downloadURL.Path, "/downloads/beat-agent/") {
		return errors.New("升级包地址不在允许范围内")
	}
	if downloadURL.Path != "/downloads/beat-agent/"+upgrade.GetArch() {
		return errors.New("升级包架构路径与目标架构不一致")
	}
	return nil
}

func allowedUpgradeTarget(path string) bool {
	return path == "/usr/local/bin/beat-agent"
}

func (r *Runtime) downloadUpgrade(ctx context.Context, upgrade *beatv1.AgentUpgrade, directory string) (string, error) {
	target := strings.TrimSpace(upgrade.GetDownloadUrl())
	server, err := url.Parse(strings.TrimSpace(r.cfg.ServerURL))
	if err != nil {
		return "", errors.New("Agent 服务端地址无效")
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return "", errors.New("升级包地址无效")
	}
	if !parsed.IsAbs() {
		parsed = server.ResolveReference(parsed)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: r.cfg.InsecureSkipVerify}} //nolint:gosec -- explicitly enabled by operator flag
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("升级下载不允许重定向")
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", errors.New("创建升级下载请求失败")
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("下载 Agent 安装包失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 Agent 安装包失败: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxAgentUpgradeSize {
		return "", errors.New("Agent 安装包超过大小限制")
	}
	file, err := os.CreateTemp(directory, ".beat-agent-upgrade-*")
	if err != nil {
		return "", fmt.Errorf("创建升级临时文件失败: %w", err)
	}
	path := file.Name()
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxAgentUpgradeSize+1))
	if err != nil {
		return "", fmt.Errorf("写入 Agent 安装包失败: %w", err)
	}
	if written != upgrade.GetSize() {
		return "", fmt.Errorf("Agent 安装包大小不一致: 期望 %d，实际 %d", upgrade.GetSize(), written)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("同步 Agent 安装包失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("关闭 Agent 安装包失败: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, upgrade.GetSha256()) {
		return "", errors.New("Agent 安装包 SHA-256 校验失败")
	}
	if err := os.Chmod(path, 0o750); err != nil {
		return "", fmt.Errorf("设置 Agent 可执行权限失败: %w", err)
	}
	success = true
	return path, nil
}

func replaceAgentBinary(executable, staged string) error {
	previous := executable + ".previous"
	_ = os.Remove(previous)
	if err := os.Rename(executable, previous); err != nil {
		return err
	}
	if err := os.Rename(staged, executable); err != nil {
		_ = os.Rename(previous, executable)
		return err
	}
	return nil
}

func (r *Runtime) emitUpgradeEvent(ctx context.Context, operationID, phase, message string) {
	if strings.TrimSpace(operationID) == "" {
		return
	}
	eventCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := r.client.SendUpgradeEvent(eventCtx, &beatv1.UpgradeEvent{
		OperationId: operationID, Phase: phase, Message: message,
	}); err != nil {
		r.cfg.Logger.Warn("agent_upgrade_event_send_failed", "operation_id", operationID, "phase", phase, "error", err)
	}
}

func (r *Runtime) restartRequested() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restart
}
