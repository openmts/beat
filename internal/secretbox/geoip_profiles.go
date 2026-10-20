package secretbox

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

type GeoIPFingerprinter interface {
	GeoIPFingerprint([]byte) (string, error)
	VerifyGeoIPFingerprint([]byte, string) bool
}

func GeoIPSourceAAD(id, mode string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "geoip_source", RowID: id, Field: "source", Revision: mode, PayloadVersion: "v1"}
}

func GeoIPCredentialAAD() ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "geoip_provider", RowID: "global", Field: "credential", PayloadVersion: "v1"}
}

func (box *DataCipher) GeoIPFingerprint(content []byte) (string, error) {
	if len(content) > MaximumSecretBytes {
		return "", errors.New("geoip source exceeds size limit")
	}
	// 从内部派生键再次作域分离，不复用 operation 指纹输出域。
	key, err := hkdf.Key(sha256.New, box.operationKey, nil, "beat/geoip/fingerprint/v1", sha256.Size)
	if err != nil {
		return "", err
	}
	defer clear(key)
	mac := hmac.New(sha256.New, key)
	if _, err = mac.Write(content); err != nil {
		return "", err
	}
	return hex.EncodeToString(box.id[:]) + ":" + hex.EncodeToString(mac.Sum(nil)), nil
}

func (box *DataCipher) VerifyGeoIPFingerprint(content []byte, expected string) bool {
	actual, err := box.GeoIPFingerprint(content)
	return err == nil && hmac.Equal([]byte(actual), []byte(expected))
}
