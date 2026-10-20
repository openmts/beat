package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/releasesignature"
)

const maximumRolloutConfigurationBytes = 64 << 10

const rolloutConfigurationVersion = 1

// 发布信任模式：缺省（或 "sigstore"）要求完整的 Sigstore 策略；"pinned_ed25519" 只锚定
// 配置里列出的固定 Ed25519 公钥，供没有 Sigstore 信任锚的离线实例显式启用。
const (
	rolloutSignatureModeSigstore = "sigstore"
	rolloutSignatureModePinned   = "pinned_ed25519"
)

var errRolloutConfiguration = errors.New("invalid Agent rollout startup configuration")

type rolloutConfiguration struct {
	SchemaVersion int                   `json:"schema_version"`
	Enabled       bool                  `json:"enabled"`
	SignatureMode string                `json:"signature_mode"`
	Keys          []rolloutTrustKey     `json:"keys"`
	Sigstore      rolloutSignatureTrust `json:"sigstore"`
}

type rolloutTrustKey struct {
	ID        string            `json:"id"`
	PublicKey ed25519.PublicKey `json:"public_key"`
	NotBefore time.Time         `json:"not_before"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type rolloutSignatureTrust struct {
	Executable          string `json:"executable"`
	TrustedRootFile     string `json:"trusted_root_file"`
	CertificateIdentity string `json:"certificate_identity"`
	ReleaseWorkflow     string `json:"release_workflow"`
	OIDCIssuer          string `json:"oidc_issuer"`
}

func readRolloutConfiguration(path string) (rolloutConfiguration, error) {
	if path == "" {
		return rolloutConfiguration{}, nil
	}
	content, err := releasesignature.ReadTrustFile(path)
	if err != nil || len(content) > maximumRolloutConfigurationBytes {
		return rolloutConfiguration{}, errRolloutConfiguration
	}
	var config rolloutConfiguration
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil || config.SchemaVersion != rolloutConfigurationVersion {
		return config, errRolloutConfiguration
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return config, errRolloutConfiguration
	}
	if err := config.normalizeSignatureMode(); err != nil {
		return config, err
	}
	return config, nil
}

// normalizeSignatureMode 把缺省模式补成 sigstore，并拒绝含糊组合：固定公钥模式必须有公钥，
// 且不能同时提供 Sigstore 策略块，否则运维以为两种锚点都在生效。
func (config *rolloutConfiguration) normalizeSignatureMode() error {
	switch config.SignatureMode {
	case "":
		config.SignatureMode = rolloutSignatureModeSigstore
	case rolloutSignatureModeSigstore:
	case rolloutSignatureModePinned:
		if len(config.Keys) == 0 || config.Sigstore != (rolloutSignatureTrust{}) {
			return errRolloutConfiguration
		}
	default:
		return errRolloutConfiguration
	}
	return nil
}

func (config rolloutConfiguration) trustedKeys() []agentrollout.TrustedKey {
	keys := make([]agentrollout.TrustedKey, 0, len(config.Keys))
	for _, key := range config.Keys {
		keys = append(keys, agentrollout.TrustedKey{ID: key.ID, PublicKey: key.PublicKey, NotBefore: key.NotBefore, ExpiresAt: key.ExpiresAt})
	}
	return keys
}
