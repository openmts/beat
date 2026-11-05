package agentjoin

import (
	"fmt"
	"regexp"
	"strings"
)

const releaseRepository = "openmts/beat"

var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// AgentDownloadCommand 生成与当前服务端版本匹配的 Linux Agent 下载脚本。
// 开发构建没有对应的 GitHub Release，返回空字符串由界面提示改用手工安装。
func AgentDownloadCommand(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !releaseVersionPattern.MatchString(version) || strings.Contains(version, "+") || strings.HasSuffix(version, "-dev") {
		return ""
	}
	return fmt.Sprintf(`set -euo pipefail
umask 077
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) printf 'unsupported architecture: %%s\n' "$(uname -m)" >&2; exit 1 ;;
esac
curl --fail --show-error --location --retry 3 --proto '=https' --proto-redir '=https' \
  --output ./beat-agent \
  "https://github.com/%s/releases/download/v%s/beat-agent_linux_${arch}"
chmod 0700 ./beat-agent`, releaseRepository, version)
}
