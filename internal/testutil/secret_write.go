package testutil

import (
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

type SecretWrite struct {
	Plaintext []byte
	AAD       secretbox.ResourceAAD
	Persist   func([]byte) error
}

// WriteSecret 在与生产相同的加密/提交窗口中建立测试密文。
func WriteSecret(t testing.TB, secrets secretbox.ResourceService, write SecretWrite) {
	t.Helper()
	err := secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		ciphertext, err := access.Seal(write.Plaintext, write.AAD)
		if err != nil {
			return err
		}
		return write.Persist(ciphertext)
	})
	if err != nil {
		t.Fatalf("write encrypted fixture: %v", err)
	}
}
