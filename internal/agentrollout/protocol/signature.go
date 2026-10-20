package protocol

import (
	"crypto/ed25519"
	"errors"
	"time"
)

var ErrManifest = errors.New("release manifest is invalid or expired")

var ErrSignature = errors.New("release signature is not trusted")

// SigningTrust 只来自Agent或Server带外安装的公钥锚，不能从manifest自身构造。
type SigningTrust struct {
	KeyID                string
	PublicKey            ed25519.PublicKey
	NotBefore, ExpiresAt time.Time
	Revoked              bool
}

// VerifyManifestSignature 纯校验canonical payload、Ed25519、时间和transition。
// Sigstore证据由Server额外校验；本函数不会联网、访问SQLite或执行外部程序。
func VerifyManifestSignature(signed SignedManifest, trust SigningTrust, now time.Time) (Manifest, error) {
	manifest, err := DecodeManifest(signed.Payload)
	if err != nil {
		return manifest, err
	}
	if err := ValidateManifest(manifest, now); err != nil {
		return manifest, err
	}
	if manifest.SigningKeyID != trust.KeyID {
		return manifest, ErrSignature
	}
	if err := verifyPinnedSignature(signed, trust, now); err != nil {
		return manifest, err
	}
	if err := VerifyTransition(trust.PublicKey, manifest.NextKey, now); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func verifyPinnedSignature(signed SignedManifest, trust SigningTrust, now time.Time) error {
	if trust.Revoked || now.Before(trust.NotBefore) || !now.Before(trust.ExpiresAt) || len(trust.PublicKey) != ed25519.PublicKeySize {
		return ErrSignature
	}
	if !ed25519.Verify(trust.PublicKey, signed.Payload, signed.Signature) {
		return ErrSignature
	}
	return nil
}
