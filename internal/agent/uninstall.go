package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExecuteUninstall removes the installed Agent command, systemd unit, YAML
// configuration, legacy environment file and runtime data. It deliberately
// refuses to delete binaries outside the
// standard installation directories so development runs cannot uninstall the
// source tree by accident.
func ExecuteUninstall(stateFile string) (string, error) {
	if os.Geteuid() != 0 {
		return "", fmt.Errorf("卸载 Agent 需要 root 权限")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("读取 Agent 可执行文件路径失败: %w", err)
	}
	executable = filepath.Clean(executable)
	if !allowedAgentBinary(executable) {
		return "", fmt.Errorf("拒绝删除非标准安装路径的 Agent: %s", executable)
	}

	_ = exec.Command("systemctl", "disable", "beat-agent").Run()
	removed := make([]string, 0, 5)
	for _, file := range []string{
		"/etc/systemd/system/beat-agent.service",
		"/etc/beat/agent.yaml",
		agentStatePath,
		"/etc/beat/beat-agent.env",
		"/usr/local/bin/beat-agent",
		"/opt/beat/agent/beat-agent.previous",
		"/opt/beat/agent/beat-agent",
		filepath.Clean(stateFile),
		executable,
	} {
		if file == "." || file == string(filepath.Separator) || file == "" {
			continue
		}
		if err := os.Remove(file); err == nil {
			removed = append(removed, file)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("删除 %s 失败: %w", file, err)
		}
	}
	for _, dir := range []string{"/opt/beat/agent", "/opt/beat/bin", "/opt/beat"} {
		_ = os.Remove(dir)
	}
	legacyStateDirectory := "/var/lib/beat-agent"
	if err := os.RemoveAll(legacyStateDirectory); err == nil {
		removed = append(removed, legacyStateDirectory)
	} else {
		return "", fmt.Errorf("删除 %s 失败: %w", legacyStateDirectory, err)
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	if len(removed) == 0 {
		return "未发现需要清理的 Agent 文件", nil
	}
	return "已删除: " + strings.Join(removed, ", "), nil
}

func allowedAgentBinary(path string) bool {
	return path == "/usr/local/bin/beat-agent"
}
