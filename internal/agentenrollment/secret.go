// Package agentenrollment 实现自动发现式 Agent 入网的纯计算部分：秘密生成、策略校验与
// 状态推导。该包不接触 SQLite、HTTP 或日志，便于逐条验证设计文档里的安全不变量。
package agentenrollment

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// 秘密前缀按用途区分：授权秘密、请求令牌与认领票据互不通用，日志脱敏也据此识别。
const (
	GrantSecretPrefix  = "beat_enroll_v1_"
	RequestTokenPrefix = "beat_request_v1_"
	ClaimTicketPrefix  = "beat_claim_v1_"
)

const (
	// secretBytes 是每枚秘密的随机字节数。
	secretBytes = 32
	// displayCharacters 是落库前缀保留的编码字符数，用于列表展示与人工比对。
	displayCharacters = 8
)

// ErrInvalidSecret 表示明文秘密不满足对应用途的前缀、长度或编码约束。
var ErrInvalidSecret = errors.New("invalid enrollment secret")

// Secret 是秘密的落库形态：明文只在创建或轮换响应中出现一次。
type Secret struct {
	Plaintext string
	Prefix    string
	Hash      []byte
}

// GenerateGrantSecret 生成 owner 创建的入网授权秘密。
func GenerateGrantSecret() (Secret, error) { return GenerateGrantSecretFrom(rand.Reader) }

// GenerateGrantSecretFrom 供测试注入随机源。
func GenerateGrantSecretFrom(reader io.Reader) (Secret, error) {
	return generateSecret(reader, GrantSecretPrefix)
}

// GenerateRequestToken 生成只在创建响应里出现一次的请求令牌。
func GenerateRequestToken() (Secret, error) { return GenerateRequestTokenFrom(rand.Reader) }

// GenerateRequestTokenFrom 供测试注入随机源。
func GenerateRequestTokenFrom(reader io.Reader) (Secret, error) {
	return generateSecret(reader, RequestTokenPrefix)
}

// GenerateClaimTicket 生成两分钟有效、单次使用的认领票据。
func GenerateClaimTicket() (Secret, error) { return GenerateClaimTicketFrom(rand.Reader) }

// GenerateClaimTicketFrom 供测试注入随机源。
func GenerateClaimTicketFrom(reader io.Reader) (Secret, error) {
	return generateSecret(reader, ClaimTicketPrefix)
}

// DescribeSecret 校验既有明文并推导落库所需的哈希与前缀；前缀不匹配或长度不对即拒绝。
func DescribeSecret(prefix, plaintext string) (Secret, error) {
	encoded, ok := encodedBody(prefix, plaintext)
	if !ok {
		return Secret{}, fmt.Errorf("%w: unexpected body", ErrInvalidSecret)
	}
	digest := SecretDigest(plaintext)
	return Secret{Plaintext: plaintext, Prefix: prefix + encoded[:displayCharacters], Hash: digest[:]}, nil
}

// SecretDigest 返回明文秘密的 SHA-256。
func SecretDigest(plaintext string) [sha256.Size]byte { return sha256.Sum256([]byte(plaintext)) }

// SecretMatches 以常量时间比较落库哈希与明文秘密。
func SecretMatches(hash []byte, plaintext string) bool {
	if len(hash) != sha256.Size {
		return false
	}
	digest := SecretDigest(plaintext)
	return subtle.ConstantTimeCompare(hash, digest[:]) == 1
}

func generateSecret(reader io.Reader, prefix string) (Secret, error) {
	random := make([]byte, secretBytes)
	if _, err := io.ReadFull(reader, random); err != nil {
		return Secret{}, fmt.Errorf("generate enrollment secret: %w", err)
	}
	return DescribeSecret(prefix, prefix+base64.RawURLEncoding.EncodeToString(random))
}

// encodedBody 返回去掉前缀后的 Base64URL 主体，并校验长度、前缀与解码后的字节数。
func encodedBody(prefix, plaintext string) (string, bool) {
	encoded := strings.TrimPrefix(plaintext, prefix)
	if encoded == plaintext || len(encoded) < displayCharacters {
		return "", false
	}
	random, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(random) != secretBytes {
		return "", false
	}
	return encoded, true
}
