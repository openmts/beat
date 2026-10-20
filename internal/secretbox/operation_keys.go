package secretbox

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// OperationFingerprinter 复用 wrapped data key；调用只能发生在生命周期写窗口内。
type OperationFingerprinter interface {
	OperationFingerprint([]byte) (string, error)
	VerifyOperationFingerprint([]byte, string) bool
}

func deriveOperationKey(material []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, material, nil, "beat/operations/fingerprint/v1", sha256.Size)
}

func (box *DataCipher) OperationFingerprint(content []byte) (string, error) {
	if len(content) > MaximumSecretBytes {
		return "", errors.New("operation fingerprint exceeds size limit")
	}
	mac := hmac.New(sha256.New, box.operationKey)
	_, err := mac.Write(content)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(box.id[:]) + ":" + hex.EncodeToString(mac.Sum(nil)), nil
}

func (box *DataCipher) VerifyOperationFingerprint(content []byte, fingerprint string) bool {
	actual, err := box.OperationFingerprint(content)
	return err == nil && hmac.Equal([]byte(actual), []byte(fingerprint))
}
