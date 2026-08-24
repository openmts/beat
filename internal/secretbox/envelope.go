package secretbox

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// 信封格式 1 规范：magic + 版本 + 算法 + 数据键 ID（根密钥指纹）+ nonce + 密文。
// 仅在 migration v5 将其转换为包装数据键信封之前用于已注册的 OIDC 资源。
const (
	envelopeMagic   = "BEATENC1"
	envelopeFormat1 = 1
	algorithmAESGCM = 1
	keyIDBytes      = 8
	maxAADBytes     = 1024
	maxEnvelopeSize = 1 << 20 // 1 MiB 上限
)

// ResourceAAD 是资源绑定的认证数据，按注册的 profile 构造。字段替换、
// 行替换或类型替换都会导致 AAD 不匹配而解密失败。
type ResourceAAD struct {
	Domain         string
	Namespace      string
	RowID          string
	Field          string
	Revision       string
	PayloadVersion string
}

// Marshal 将 AAD 编码为长度前缀元组，供解密方重建。
func (a ResourceAAD) Marshal() ([]byte, error) {
	components := []string{a.Domain, a.Namespace, a.RowID, a.Field, a.Revision, a.PayloadVersion}
	total := 0
	for _, component := range components {
		total += 2 + len(component)
	}
	if total > maxAADBytes {
		return nil, errors.New("resource AAD exceeds size limit")
	}
	out := make([]byte, 0, total)
	for _, component := range components {
		out = binary.BigEndian.AppendUint16(out, uint16(len(component)))
		out = append(out, component...)
	}
	return out, nil
}

// envelope 是格式 1 的严格二进制结构。
type envelope struct {
	KeyID      [keyIDBytes]byte
	Nonce      []byte
	Ciphertext []byte
}

func encodeEnvelope(keyID [keyIDBytes]byte, nonce, sealed []byte) []byte {
	out := make([]byte, 0, len(envelopeMagic)+1+1+keyIDBytes+len(nonce)+len(sealed))
	out = append(out, envelopeMagic...)
	out = append(out, envelopeFormat1, algorithmAESGCM)
	out = append(out, keyID[:]...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out
}

func decodeEnvelope(raw []byte) (envelope, error) {
	header := len(envelopeMagic) + 2 + keyIDBytes
	if len(raw) < header+12+16 {
		return envelope{}, errors.New("envelope is too short")
	}
	if string(raw[:len(envelopeMagic)]) != envelopeMagic {
		return envelope{}, errors.New("envelope magic mismatch")
	}
	if raw[len(envelopeMagic)] != envelopeFormat1 {
		return envelope{}, fmt.Errorf("unsupported envelope version %d", raw[len(envelopeMagic)])
	}
	if raw[len(envelopeMagic)+1] != algorithmAESGCM {
		return envelope{}, fmt.Errorf("unsupported envelope algorithm %d", raw[len(envelopeMagic)+1])
	}
	var result envelope
	copy(result.KeyID[:], raw[len(envelopeMagic)+2:header])
	result.Nonce = raw[header : header+12]
	result.Ciphertext = raw[header+12:]
	return result, nil
}

// keyFingerprint 派生根密钥指纹作为信封的数据键 ID。
func keyFingerprint(key []byte) [keyIDBytes]byte {
	digest := sha256.Sum256(key)
	var fingerprint [keyIDBytes]byte
	copy(fingerprint[:], digest[:keyIDBytes])
	return fingerprint
}

// EncryptEnvelope 使用格式 1 信封与资源绑定 AAD 加密。
func (manager *Manager) EncryptEnvelope(plaintext []byte, aad ResourceAAD) ([]byte, error) {
	if len(plaintext) > maxEnvelopeSize {
		return nil, errors.New("plaintext exceeds envelope size limit")
	}
	nonce := make([]byte, manager.aead.NonceSize())
	manager.mu.Lock()
	_, err := manager.random.Read(nonce)
	manager.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("generate envelope nonce: %w", err)
	}
	encoded, err := aad.Marshal()
	if err != nil {
		return nil, err
	}
	sealed := manager.aead.Seal(nil, nonce, plaintext, encoded)
	return encodeEnvelope(manager.fingerprint, nonce, sealed), nil
}

// DecryptEnvelope 校验格式 1 信封并解密；AAD 或数据键指纹不匹配即失败。
func (manager *Manager) DecryptEnvelope(ciphertext []byte, aad ResourceAAD) ([]byte, error) {
	if len(ciphertext) > maxEnvelopeSize {
		return nil, errors.New("envelope exceeds size limit")
	}
	parsed, err := decodeEnvelope(ciphertext)
	if err != nil {
		return nil, err
	}
	if parsed.KeyID != manager.fingerprint {
		return nil, errors.New("envelope key id does not match root key")
	}
	encoded, err := aad.Marshal()
	if err != nil {
		return nil, err
	}
	plaintext, err := manager.aead.Open(nil, parsed.Nonce, parsed.Ciphertext, encoded)
	if err != nil {
		return nil, fmt.Errorf("decrypt envelope: %w", err)
	}
	return plaintext, nil
}
