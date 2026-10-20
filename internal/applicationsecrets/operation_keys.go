package applicationsecrets

import "strings"

func (ring *keyring) OperationFingerprint(content []byte) (string, error) {
	cipher := ring.ciphers[ring.active]
	if cipher == nil {
		return "", ErrUnhealthy
	}
	return cipher.OperationFingerprint(content)
}

func (ring *keyring) VerifyOperationFingerprint(content []byte, fingerprint string) bool {
	id, _, ok := strings.Cut(fingerprint, ":")
	if !ok {
		return false
	}
	cipher := ring.ciphers[id]
	return cipher != nil && cipher.VerifyOperationFingerprint(content, fingerprint)
}
