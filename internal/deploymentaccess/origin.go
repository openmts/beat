package deploymentaccess

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// OriginPolicy 是公共 REST/WebSocket 允许来源的唯一契约。
// wildcard 表示允许任意来源；exact 表示只允许归一化后完全一致的来源。
type OriginPolicy struct {
	wildcard bool
	exact    map[string]struct{}
}

const (
	// OriginModeAll 允许任意浏览器来源，仅用于无凭据的公共读接口。
	OriginModeAll = "all"
	// OriginModeConfigured 只允许显式配置的来源。
	OriginModeConfigured = "configured"
	// OriginModeSameOrigin 未配置来源列表，只接受服务端自身来源。
	OriginModeSameOrigin = "same_origin"
)

const (
	maxOriginBytes = 255
	maxOrigins     = 64
)

var (
	// ErrOriginWildcardMixed 表示通配符与精确来源混用。
	ErrOriginWildcardMixed = errors.New("deployment access: wildcard origin cannot be combined with explicit origins")
	// ErrOriginInvalid 表示某个来源无法归一化。
	ErrOriginInvalid = errors.New("deployment access: origin is invalid")
	// ErrOriginDuplicate 表示归一化后出现重复来源。
	ErrOriginDuplicate = errors.New("deployment access: origin is duplicated")
)

// ParseOrigins 解析逗号分隔的精确来源、单个 `*` 或空值。
// 空值表示未配置：调用方需要按同源处理。
func ParseOrigins(value string) (OriginPolicy, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return OriginPolicy{exact: map[string]struct{}{}}, nil
	}
	if trimmed == "*" {
		return OriginPolicy{wildcard: true, exact: map[string]struct{}{}}, nil
	}
	parts := strings.Split(trimmed, ",")
	if len(parts) > maxOrigins {
		return OriginPolicy{}, ErrOriginInvalid
	}
	policy := OriginPolicy{exact: make(map[string]struct{}, len(parts))}
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if candidate == "*" {
			return OriginPolicy{}, ErrOriginWildcardMixed
		}
		normalized, err := NormalizeOrigin(candidate)
		if err != nil {
			return OriginPolicy{}, err
		}
		if _, exists := policy.exact[normalized]; exists {
			return OriginPolicy{}, ErrOriginDuplicate
		}
		policy.exact[normalized] = struct{}{}
	}
	return policy, nil
}

// NormalizeOrigin 只接受带 scheme 与 authority 的来源，并去掉默认端口。
func NormalizeOrigin(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maxOriginBytes || containsControl(trimmed) {
		return "", ErrOriginInvalid
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrOriginInvalid
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrOriginInvalid
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrOriginInvalid
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrOriginInvalid
	}
	if parsed.Hostname() == "" {
		return "", ErrOriginInvalid
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority += ":" + port
	}
	return parsed.Scheme + "://" + authority, nil
}

// Wildcard 报告该策略是否允许任意来源。
func (policy OriginPolicy) Wildcard() bool { return policy.wildcard }

// Count 返回归一化后的来源数量。
func (policy OriginPolicy) Count() int { return len(policy.exact) }

// Mode 返回可公开展示的策略模式，不暴露来源列表本身。
func (policy OriginPolicy) Mode() string {
	if policy.wildcard {
		return OriginModeAll
	}
	if len(policy.exact) == 0 {
		return OriginModeSameOrigin
	}
	return OriginModeConfigured
}

// Allows 判断一个浏览器来源是否被允许。缺少来源时由调用方决定。
func (policy OriginPolicy) Allows(origin string) bool {
	if policy.wildcard {
		return true
	}
	normalized, err := NormalizeOrigin(origin)
	if err != nil {
		return false
	}
	_, ok := policy.exact[normalized]
	return ok
}

// Origins 返回排序后的来源列表，仅用于服务端内部比较与测试。
func (policy OriginPolicy) Origins() []string {
	values := make([]string, 0, len(policy.exact))
	for origin := range policy.exact {
		values = append(values, origin)
	}
	sort.Strings(values)
	return values
}

func containsControl(value string) bool {
	for _, symbol := range value {
		if symbol < 0x20 || symbol == 0x7f || unicode.IsSpace(symbol) {
			return true
		}
	}
	return false
}
