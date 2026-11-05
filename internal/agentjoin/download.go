package agentjoin

import (
	"fmt"
	"regexp"
	"strings"
)

const releaseRepository = "openmts/beat"

var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// AgentDownloadCommand 生成与当前服务端版本匹配的 Linux Agent 下载命令。
// 开发构建没有对应的 GitHub Release，返回空字符串由界面提示改用手工安装。
func AgentDownloadCommand(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !releaseVersionPattern.MatchString(version) || strings.Contains(version, "+") || strings.HasSuffix(version, "-dev") {
		return ""
	}
	return fmt.Sprintf(`sudo curl -fsSL --retry 3 --proto '=https' --proto-redir '=https' "https://github.com/%s/releases/download/v%s/beat-agent_linux_$(uname -m)" -o /usr/local/bin/beat-agent && sudo chmod 0700 /usr/local/bin/beat-agent`, releaseRepository, version)
}
