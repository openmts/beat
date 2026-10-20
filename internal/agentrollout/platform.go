package agentrollout

import "strings"

// 节点表的 os/arch 来自宿主 uname（例如 x86_64、aarch64），而发布清单与 Agent 自报能力
// 使用 Go 的 GOARCH 命名（amd64、arm64）。两侧比较前统一到 GOARCH 口径，避免真实主机
// 因为命名差异被判成 platform_changed。
var architectureAliases = map[string]string{
	"amd64": "amd64", "x86_64": "amd64", "x64": "amd64",
	"arm64": "arm64", "aarch64": "arm64",
	"386": "386", "i386": "386", "i486": "386", "i586": "386", "i686": "386", "x86": "386",
	"arm": "arm", "armv6l": "arm", "armv7l": "arm",
	"loong64": "loong64", "loongarch64": "loong64",
}

func sameArchitecture(left, right string) bool {
	left, right = normalizeArchitecture(left), normalizeArchitecture(right)
	if left == "" || right == "" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func normalizeArchitecture(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, known := architectureAliases[value]; known {
		return normalized
	}
	return ""
}
