// Package releasemanifest 把发布描述文件编译成可签署的 canonical manifest，
// 并提供 Ed25519 私钥解析与信任文件生成。它只处理本地文件，不访问服务端。
package releasemanifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrSpec 表示发布描述文件、私钥或输出内容不合法。
var ErrSpec = errors.New("release manifest input is invalid")

// MinimumSeedBytes/MaximumKeyBytes 约束接受的私钥长度：32 字节种子或 64 字节私钥。
const (
	seedBytes = ed25519.SeedSize
	keyBytes  = ed25519.PrivateKeySize
)

// GenerateKey 生成一把发布私钥；调用方负责以 0600 落盘种子。
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, ErrSpec
	}
	return public, private, nil
}

// EncodeSeed 把私钥的 32 字节种子编成单行 base64，便于带外传递与权限控制。
func EncodeSeed(private ed25519.PrivateKey) (string, error) {
	if len(private) != keyBytes {
		return "", ErrSpec
	}
	return base64.StdEncoding.EncodeToString(private.Seed()), nil
}

// ParsePrivateKey 接受 base64 或十六进制编码的 32 字节种子或 64 字节私钥。
func ParsePrivateKey(content []byte) (ed25519.PrivateKey, error) {
	value := strings.TrimSpace(string(content))
	if value == "" {
		return nil, ErrSpec
	}
	raw, ok := decodeKeyMaterial(value)
	if !ok {
		return nil, ErrSpec
	}
	if len(raw) == seedBytes {
		return ed25519.NewKeyFromSeed(raw), nil
	}
	return ed25519.PrivateKey(raw), nil
}

// decodeKeyMaterial 依次尝试 base64 与十六进制，返回第一个长度合法的解码结果。
// 长度必须参与判定：64 个十六进制字符本身就是合法 base64，先解 base64 会得到 48 字节，
// 十六进制种子因此永远读不进来。
func decodeKeyMaterial(value string) ([]byte, bool) {
	if raw, err := base64.StdEncoding.DecodeString(value); err == nil && validKeySize(len(raw)) {
		return raw, true
	}
	if raw, err := hex.DecodeString(value); err == nil && validKeySize(len(raw)) {
		return raw, true
	}
	return nil, false
}

// validKeySize 只接受 32 字节种子与 64 字节私钥两种长度。
func validKeySize(size int) bool {
	return size == seedBytes || size == keyBytes
}
