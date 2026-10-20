package applicationsecrets

import "github.com/beat/backend/internal/secretbox"

func (ring *keyring) SealOperationFrame(plain []byte, frame secretbox.OperationFrame) ([]byte, error) {
	cipher := ring.ciphers[ring.active]
	if cipher == nil {
		return nil, ErrUnhealthy
	}
	return cipher.SealOperationFrame(plain, frame)
}

func (ring *keyring) OpenOperationFrame(sealed []byte, frame secretbox.OperationFrame) ([]byte, error) {
	id, err := secretbox.OperationFrameKeyID(sealed)
	if err != nil {
		return nil, ErrCiphertext
	}
	cipher := ring.ciphers[id]
	if cipher == nil {
		return nil, ErrUnhealthy
	}
	return cipher.OpenOperationFrame(sealed, frame)
}
