package agentenrollment

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

// 授权策略边界：与设计文档 “Grant secret” 一节逐条对应，服务端与工具共用同一套校验。
const (
	MaxGrantNameLength       = 200
	MaxGrantNodeNamePrefix   = 64
	MaxGrantSourceCIDRs      = 16
	MaxGrantHostnamePattern  = 256
	MinimumGrantEnrollments  = 1
	MaximumGrantEnrollments  = 1000
	MaxDerivedNodeNameLength = 64
	MaxClaimRecoveryAttempts = 3
	ClaimRecoveryWindow      = 15 * time.Minute
	maximumGrantLifetime     = 10 * 365 * 24 * time.Hour
	automaticNameSeparator   = "-"
)

// ErrInvalidGrantPolicy 表示授权策略不满足设计边界。
var ErrInvalidGrantPolicy = errors.New("invalid enrollment grant policy")

// ApprovalMode 决定请求创建后是否立即按策略审批。
type ApprovalMode string

const (
	ApprovalManual    ApprovalMode = "manual"
	ApprovalAutomatic ApprovalMode = "automatic"
)

// Valid 校验审批模式取值。
func (mode ApprovalMode) Valid() bool {
	return mode == ApprovalManual || mode == ApprovalAutomatic
}

// GrantPolicy 是 owner 预先固定的入网策略；请求只能在其范围内成立。
type GrantPolicy struct {
	Name               string
	Mode               ApprovalMode
	MaximumEnrollments int
	DefaultGroupID     string
	NodeNamePrefix     string
	Tags               []string
	Remarks            string
	Public             bool
	SSHPort            int
	UseSourceIPAsHost  bool
	SourceCIDRs        []string
	HostnamePattern    string
	ClaimRecovery      bool
	Enabled            bool
	ExpiresAt          time.Time
}

// Validate 逐项校验授权策略：名称与模式、容量与有效期、展示字段与来源策略。
func (policy GrantPolicy) Validate(now time.Time) error {
	if err := policy.validateIdentity(); err != nil {
		return err
	}
	if err := policy.validateCapacity(now); err != nil {
		return err
	}
	return policy.validatePresentation()
}

func (policy GrantPolicy) validateIdentity() error {
	name := strings.TrimSpace(policy.Name)
	if name == "" || utf8.RuneCountInString(name) > MaxGrantNameLength {
		return fmt.Errorf("%w: grant name must be 1-%d characters", ErrInvalidGrantPolicy, MaxGrantNameLength)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: grant name contains control characters", ErrInvalidGrantPolicy)
	}
	if !policy.Mode.Valid() {
		return fmt.Errorf("%w: unknown approval mode", ErrInvalidGrantPolicy)
	}
	prefix := strings.TrimSpace(policy.NodeNamePrefix)
	if policy.Mode == ApprovalAutomatic && prefix == "" {
		return fmt.Errorf("%w: automatic grants need a node name prefix", ErrInvalidGrantPolicy)
	}
	if utf8.RuneCountInString(prefix) > MaxGrantNodeNamePrefix {
		return fmt.Errorf("%w: node name prefix must be at most %d characters", ErrInvalidGrantPolicy, MaxGrantNodeNamePrefix)
	}
	return nil
}

func (policy GrantPolicy) validateCapacity(now time.Time) error {
	if strings.TrimSpace(policy.DefaultGroupID) == "" {
		return fmt.Errorf("%w: default group is required", ErrInvalidGrantPolicy)
	}
	if policy.MaximumEnrollments < MinimumGrantEnrollments || policy.MaximumEnrollments > MaximumGrantEnrollments {
		return fmt.Errorf("%w: maximum enrollments must be %d-%d", ErrInvalidGrantPolicy,
			MinimumGrantEnrollments, MaximumGrantEnrollments)
	}
	if !policy.ExpiresAt.After(now) {
		return fmt.Errorf("%w: grant expiry must be in the future", ErrInvalidGrantPolicy)
	}
	if policy.ExpiresAt.After(now.Add(maximumGrantLifetime)) {
		return fmt.Errorf("%w: grant expiry is too far in the future", ErrInvalidGrantPolicy)
	}
	return nil
}

func (policy GrantPolicy) validatePresentation() error {
	if _, err := model.NormalizeNodeTags(policy.Tags); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidGrantPolicy, err)
	}
	if utf8.RuneCountInString(policy.Remarks) > model.MaxNodePrivateRemarkLength {
		return fmt.Errorf("%w: grant remarks are too long", ErrInvalidGrantPolicy)
	}
	if policy.SSHPort < 1 || policy.SSHPort > 65535 {
		return fmt.Errorf("%w: SSH port must be 1-65535", ErrInvalidGrantPolicy)
	}
	if len(policy.SourceCIDRs) > MaxGrantSourceCIDRs {
		return fmt.Errorf("%w: at most %d source CIDRs are allowed", ErrInvalidGrantPolicy, MaxGrantSourceCIDRs)
	}
	if _, err := CompileSourcePolicy(policy.SourceCIDRs, policy.HostnamePattern); err != nil {
		return err
	}
	return nil
}

// SourcePolicy 是规范化后的来源约束：地址按前缀匹配，主机名按锚定正则匹配。
type SourcePolicy struct {
	prefixes []netip.Prefix
	hostname *regexp.Regexp
}

// CompileSourcePolicy 编译授权来源范围；IPv4-mapped 形式被拒绝以避免同一地址两种写法。
func CompileSourcePolicy(cidrs []string, pattern string) (*SourcePolicy, error) {
	policy := &SourcePolicy{}
	for _, raw := range cidrs {
		text := strings.TrimSpace(raw)
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			return nil, fmt.Errorf("%w: source CIDR %q", ErrInvalidGrantPolicy, text)
		}
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("%w: IPv4-mapped source CIDR %q", ErrInvalidGrantPolicy, text)
		}
		policy.prefixes = append(policy.prefixes, prefix.Masked())
	}
	compiled, err := compileHostnamePattern(pattern)
	if err != nil {
		return nil, err
	}
	policy.hostname = compiled
	return policy, nil
}

func compileHostnamePattern(pattern string) (*regexp.Regexp, error) {
	text := strings.TrimSpace(pattern)
	if text == "" {
		return nil, nil
	}
	if len(text) > MaxGrantHostnamePattern {
		return nil, fmt.Errorf("%w: hostname pattern must be at most %d bytes", ErrInvalidGrantPolicy, MaxGrantHostnamePattern)
	}
	compiled, err := regexp.Compile("^(?:" + text + ")$")
	if err != nil {
		return nil, fmt.Errorf("%w: hostname pattern is invalid", ErrInvalidGrantPolicy)
	}
	return compiled, nil
}

// Unrestricted 表示该来源策略既不限制地址也不限制主机名。
func (policy *SourcePolicy) Unrestricted() bool {
	return policy == nil || (len(policy.prefixes) == 0 && policy.hostname == nil)
}

// AllowsSource 判断规范化后的来源地址是否落在授权范围内。
func (policy *SourcePolicy) AllowsSource(address netip.Addr) bool {
	if policy == nil || len(policy.prefixes) == 0 {
		return true
	}
	address = address.Unmap()
	for _, prefix := range policy.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// AllowsHostname 判断上报主机名是否匹配授权正则。
func (policy *SourcePolicy) AllowsHostname(hostname string) bool {
	if policy == nil || policy.hostname == nil {
		return true
	}
	return policy.hostname.MatchString(strings.TrimSpace(hostname))
}

// DeriveNodeName 由授权前缀与上报主机名推导自动入网节点名：只保留小写字母、数字与短横线，
// 折叠分隔符并截断到上限；结果为空时返回错误，绝不追加随机后缀。
func DeriveNodeName(prefix, hostname string) (string, error) {
	host := sanitizeNodeNamePart(hostname)
	if host == "" {
		return "", fmt.Errorf("%w: hostname cannot form a node name", ErrInvalidGrantPolicy)
	}
	parts := make([]string, 0, 2)
	if head := sanitizeNodeNamePart(prefix); head != "" {
		parts = append(parts, head)
	}
	parts = append(parts, host)
	return truncateNodeName(strings.Join(parts, automaticNameSeparator)), nil
}

// SanitizeNodeNamePart 暴露同一套字符规则，供审批界面预览自动推导结果。
func SanitizeNodeNamePart(value string) string { return sanitizeNodeNamePart(value) }

func sanitizeNodeNamePart(value string) string {
	var builder strings.Builder
	separatorPending := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if isNodeNameRune(character) {
			if separatorPending && builder.Len() > 0 {
				builder.WriteString(automaticNameSeparator)
			}
			separatorPending = false
			builder.WriteRune(character)
			continue
		}
		separatorPending = builder.Len() > 0
	}
	return strings.Trim(builder.String(), automaticNameSeparator)
}

func isNodeNameRune(character rune) bool {
	return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
}

func truncateNodeName(name string) string {
	if utf8.RuneCountInString(name) <= MaxDerivedNodeNameLength {
		return name
	}
	runes := []rune(name)[:MaxDerivedNodeNameLength]
	return strings.Trim(string(runes), automaticNameSeparator)
}
