package loginsecurity

import (
	"crypto/hmac"
	"crypto/sha256"
)

// Fingerprint 用带密钥的 HMAC 计算来源或设备指纹，使相等判断不暴露原始值。
func Fingerprint(key []byte, epoch, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(epoch))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
