package agentenrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// ProtocolVersion 绑定在每一条签名载荷上，跨协议版本的重放会被拒绝。
const ProtocolVersion = "beat-enrollment-1"

// ChallengeSize 是挑战随机数与安装随机数的字节数。
const ChallengeSize = 32

// 签名域分隔符：不同用途的载荷首字段不同，避免同一把密钥的签名被跨用途复用。
const (
	requestDomain = "beat.enrollment.request"
	statusDomain  = "beat.enrollment.status"
	claimDomain   = "beat.enrollment.claim"
)

// ErrInvalidSignature 表示签名与公钥、载荷不匹配。
var ErrInvalidSignature = errors.New("invalid enrollment signature")

// GenerateChallenge 生成挑战随机数。
func GenerateChallenge() ([]byte, error) { return GenerateChallengeFrom(rand.Reader) }

// GenerateChallengeFrom 供测试注入随机源。
func GenerateChallengeFrom(reader io.Reader) ([]byte, error) {
	buffer := make([]byte, ChallengeSize)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return nil, fmt.Errorf("generate enrollment challenge: %w", err)
	}
	return buffer, nil
}

// Fingerprint 返回 Agent 公钥的 SHA-256，即请求身份。
func Fingerprint(publicKey []byte) [sha256.Size]byte { return sha256.Sum256(publicKey) }

// Digest 返回任意载荷的 SHA-256，用于元数据摘要与秘密哈希复用同一套比较逻辑。
func Digest(payload []byte) [sha256.Size]byte { return sha256.Sum256(payload) }

// RequestBinding 是创建请求时被签名的绑定值集合。
type RequestBinding struct {
	ChallengeID string
	Challenge   []byte
	Origin      string
	Fingerprint []byte
	Metadata    []byte
	ExpiresAt   time.Time
}

// Canonical 输出长度分隔的规范编码。
func (binding RequestBinding) Canonical() []byte {
	return encodeCanonical(requestDomain,
		[]byte(binding.ChallengeID), binding.Challenge, []byte(binding.Origin),
		binding.Fingerprint, binding.Metadata, []byte(formatInstant(binding.ExpiresAt)))
}

// StatusBinding 是轮询状态时被签名的绑定值集合。
type StatusBinding struct {
	RequestID string
	Origin    string
	Timestamp time.Time
	Nonce     []byte
}

// Canonical 输出长度分隔的规范编码。
func (binding StatusBinding) Canonical() []byte {
	return encodeCanonical(statusDomain, []byte(binding.RequestID), []byte(binding.Origin),
		[]byte(formatInstant(binding.Timestamp)), binding.Nonce)
}

// ClaimBinding 是兑换节点凭据时被签名的绑定值集合。
type ClaimBinding struct {
	RequestID string
	NodeID    string
	Ticket    string
	Origin    string
	Timestamp time.Time
	Nonce     []byte
}

// Canonical 输出长度分隔的规范编码。
func (binding ClaimBinding) Canonical() []byte {
	return encodeCanonical(claimDomain, []byte(binding.RequestID), []byte(binding.NodeID),
		[]byte(binding.Ticket), []byte(binding.Origin), []byte(formatInstant(binding.Timestamp)), binding.Nonce)
}

// SignWith 用 Ed25519 私钥对规范载荷签名。
func SignWith(key ed25519.PrivateKey, payload []byte) []byte { return ed25519.Sign(key, payload) }

// VerifySignature 校验 Ed25519 签名；公钥长度非法与签名不匹配返回同一个错误。
func VerifySignature(publicKey, payload, signature []byte) error {
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return fmt.Errorf("%w: signature does not match payload", ErrInvalidSignature)
	}
	return nil
}

// encodeCanonical 逐字段写入 4 字节大端长度与内容，使任何字段边界都无法被拼接伪造。
func encodeCanonical(domain string, fields ...[]byte) []byte {
	total := len(domain)
	for _, field := range fields {
		total += len(field) + 4
	}
	encoded := make([]byte, 0, total+4)
	encoded = appendField(encoded, []byte(domain))
	for _, field := range fields {
		encoded = appendField(encoded, field)
	}
	return encoded
}

func appendField(target, field []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(field)))
	target = append(target, length[:]...)
	return append(target, field...)
}

// formatInstant 用固定精度 UTC 秒格式化时间，两端都不做本地时区换算。
func formatInstant(instant time.Time) string {
	return instant.UTC().Format(time.RFC3339)
}
