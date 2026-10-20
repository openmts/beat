package serviceprincipal

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/beat/backend/internal/model"
)

const tokenRandomBytes = 32

// Token 是一次性展示的服务令牌；Hash 用于持久化，Plaintext 只在创建响应中出现。
type Token struct {
	Plaintext string
	Prefix    string
	Hash      []byte
}

// Generate 使用系统熵源创建服务令牌。
func Generate() (Token, error) { return GenerateFrom(rand.Reader) }

// GenerateFrom 允许测试注入熵源并校验失败路径。
func GenerateFrom(reader io.Reader) (Token, error) {
	random := make([]byte, tokenRandomBytes)
	if _, err := io.ReadFull(reader, random); err != nil {
		return Token{}, fmt.Errorf("generate service token: %w", err)
	}
	plaintext := model.ServiceTokenPrefix + base64.RawURLEncoding.EncodeToString(random)
	prefix, ok := DisplayPrefix(plaintext)
	if !ok {
		return Token{}, errors.New("generate service token: invalid generated token")
	}
	hash := Digest(plaintext)
	return Token{Plaintext: plaintext, Prefix: prefix, Hash: hash[:]}, nil
}

// Digest 计算服务令牌的 SHA-256 摘要，明文从不持久化。
func Digest(token string) [sha256.Size]byte { return sha256.Sum256([]byte(token)) }

// DisplayPrefix 返回可安全展示的固定长度前缀。
func DisplayPrefix(token string) (string, bool) {
	if !strings.HasPrefix(token, model.ServiceTokenPrefix) {
		return "", false
	}
	encoded := strings.TrimPrefix(token, model.ServiceTokenPrefix)
	random, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(random) != tokenRandomBytes || len(encoded) < model.ServiceTokenDisplayLength {
		return "", false
	}
	return model.ServiceTokenPrefix + encoded[:model.ServiceTokenDisplayLength], true
}

// Matches 在固定格式解析后用常量时间比较摘要。
func Matches(hash []byte, token string) bool {
	if len(hash) != sha256.Size {
		return false
	}
	digest := Digest(token)
	return subtle.ConstantTimeCompare(hash, digest[:]) == 1
}
