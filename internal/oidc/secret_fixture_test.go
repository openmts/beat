package oidc

import (
	"context"

	"github.com/beat/backend/internal/secretbox"
)

func sealOIDCTestResource(resources secretbox.ResourceService, plaintext []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	var ciphertext []byte
	err := resources.WithWrite(context.Background(), func(access secretbox.ResourceAccess) error {
		var err error
		ciphertext, err = access.Seal(plaintext, aad)
		return err
	})
	return ciphertext, err
}
