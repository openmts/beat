package deploymentaccess

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// ErrPublicBaseURLInvalid 表示规范外部地址不可信或被配置成带路径的形式。
var ErrPublicBaseURLInvalid = errors.New("deployment access: public base url is invalid")

const maxPublicBaseURLBytes = 255

// ParsePublicBaseURL 接受 scheme 与 authority 组成的规范外部地址。
// 只有回环地址允许使用 http，其余必须是 https，且不接受路径、查询或片段。
func ParsePublicBaseURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > maxPublicBaseURLBytes || containsControl(trimmed) {
		return "", ErrPublicBaseURLInvalid
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrPublicBaseURLInvalid
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrPublicBaseURLInvalid
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !loopbackHost(parsed.Hostname()) {
			return "", ErrPublicBaseURLInvalid
		}
	default:
		return "", ErrPublicBaseURLInvalid
	}
	return NormalizeOrigin(trimmed)
}

// loopbackHost 只把回环名字与回环地址视为开发用途。
func loopbackHost(host string) bool {
	lowered := strings.ToLower(host)
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return true
	}
	address := net.ParseIP(lowered)
	return address != nil && address.IsLoopback()
}
