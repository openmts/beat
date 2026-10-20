package agentenrollment

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 上报元数据的长度上限与 SQLite 约束一一对应；越界在任何写库动作之前被拒绝。
const (
	MaxHostnameLength         = 256
	MaxAdvertisedHostLength   = 256
	MaxOSLength               = 128
	MaxArchitectureLength     = 128
	MaxAgentVersionLength     = 128
	MaxVirtualizationLength   = 256
	MaxInstallationNonceBytes = 128
	MaxSSHPort                = 65535
	MinSSHPort                = 1
)

// ErrInvalidMetadata 表示 Agent 上报的元数据越界或含控制字符。
var ErrInvalidMetadata = errors.New("invalid enrollment metadata")

// Metadata 是 Agent 自报的环境信息；全部字段都是展示建议，不参与任何授权判断。
type Metadata struct {
	Hostname          string
	AdvertisedHost    string
	OS                string
	Architecture      string
	AgentVersion      string
	Virtualization    string
	SSHPort           int
	InstallationNonce string
}

// Validate 校验每个字段的长度、可用字符与端口范围。
func (metadata Metadata) Validate() error {
	fields := []struct {
		label  string
		value  string
		limits int
	}{
		{"hostname", metadata.Hostname, MaxHostnameLength},
		{"advertised host", metadata.AdvertisedHost, MaxAdvertisedHostLength},
		{"os", metadata.OS, MaxOSLength},
		{"architecture", metadata.Architecture, MaxArchitectureLength},
		{"agent version", metadata.AgentVersion, MaxAgentVersionLength},
		{"virtualization", metadata.Virtualization, MaxVirtualizationLength},
	}
	for _, field := range fields {
		if err := validateMetadataText(field.label, field.value, field.limits); err != nil {
			return err
		}
	}
	if metadata.InstallationNonce == "" || len(metadata.InstallationNonce) > MaxInstallationNonceBytes {
		return fmt.Errorf("%w: installation nonce must be 1-%d bytes", ErrInvalidMetadata, MaxInstallationNonceBytes)
	}
	if strings.IndexFunc(metadata.InstallationNonce, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: installation nonce contains control characters", ErrInvalidMetadata)
	}
	if metadata.SSHPort < MinSSHPort || metadata.SSHPort > MaxSSHPort {
		return fmt.Errorf("%w: SSH port must be %d-%d", ErrInvalidMetadata, MinSSHPort, MaxSSHPort)
	}
	return nil
}

// validateMetadataText 只拒绝超长与控制字符；展示字段允许空值。
func validateMetadataText(label, value string, limit int) error {
	if len(value) > limit {
		return fmt.Errorf("%w: %s must be at most %d bytes", ErrInvalidMetadata, label, limit)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %s contains control characters", ErrInvalidMetadata, label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidMetadata, label)
	}
	return nil
}

// metadataDomain 是元数据规范编码的域分隔符，避免与其他签名载荷互相顶替。
const metadataDomain = "beat.enrollment.metadata"

// Canonical 输出元数据的长度分隔规范编码；Agent 与服务端必须逐字段一致。
func (metadata Metadata) Canonical() []byte {
	return encodeCanonical(metadataDomain,
		[]byte(metadata.Hostname), []byte(metadata.AdvertisedHost), []byte(metadata.OS),
		[]byte(metadata.Architecture), []byte(metadata.AgentVersion), []byte(metadata.Virtualization),
		[]byte(strconv.Itoa(metadata.SSHPort)), []byte(metadata.InstallationNonce))
}

// MetadataDigest 返回规范元数据的摘要，用于在挑战阶段先绑定元数据。
func MetadataDigest(metadata Metadata) [sha256.Size]byte { return Digest(metadata.Canonical()) }
