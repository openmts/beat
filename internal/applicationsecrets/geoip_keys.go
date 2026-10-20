package applicationsecrets

import "strings"

func (ring *keyring) GeoIPFingerprint(content []byte) (string, error) {
	cipher := ring.ciphers[ring.active]
	if cipher == nil {
		return "", ErrUnhealthy
	}
	return cipher.GeoIPFingerprint(content)
}

func (ring *keyring) VerifyGeoIPFingerprint(content []byte, fingerprint string) bool {
	id, _, ok := strings.Cut(fingerprint, ":")
	if !ok {
		return false
	}
	cipher := ring.ciphers[id]
	return cipher != nil && cipher.VerifyGeoIPFingerprint(content, fingerprint)
}
