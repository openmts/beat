package agentrollout

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/releasesignature"
)

var (
	ErrDisabled    = errors.New("agent rollout is disabled")
	ErrForbidden   = errors.New("current owner authority is required")
	ErrRecentAuth  = errors.New("recent owner authentication is required")
	ErrManifest    = protocol.ErrManifest
	ErrSignature   = protocol.ErrSignature
	ErrUnavailable = errors.New("agent rollout is unavailable")
	ErrConflict    = errors.New("rollout state conflicts with this action")
	ErrNotFound    = errors.New("rollout resource was not found")
	ErrInvalid     = errors.New("invalid rollout request")
)

type SignatureVerifier interface {
	Verify(context.Context, releasesignature.Evidence) error
}

type TrustedKey struct {
	ID                   string
	PublicKey            ed25519.PublicKey
	NotBefore, ExpiresAt time.Time
}

type Config struct {
	Database  *sql.DB
	Directory string
	Keys      []TrustedKey
	Sigstore  SignatureVerifier
	// PinnedOnly 选择「只锚定启动时固定 Ed25519 公钥」的信任模式：此时不接受 Sigstore 验证器，
	// 发布必须由清单里的 signing_key_id 对应到已配置的公钥，且不得携带 Sigstore 证据。
	// 只能由服务端启动配置显式打开，默认 false 保持原有 Sigstore 语义。
	PinnedOnly      bool
	Enabled         bool
	ServerSchema    int
	ProtocolVersion int
	Now             func() time.Time
}

type Actor struct{ UserID, SessionID, RequestID, IPAddress, UserAgent string }

type Service struct {
	transfers       chan struct{}
	db              *sql.DB
	directory       string
	keys            map[string]TrustedKey
	sigstore        SignatureVerifier
	signaturePolicy *signaturePolicySnapshot
	pinnedOnly      bool
	enabled         bool
	schema          int
	protocol        int
	now             func() time.Time
}

func New(config Config) (*Service, error) {
	return NewContext(context.Background(), config)
}

// NewContext 将启动时的信任登记绑定到服务启动上下文。
func NewContext(ctx context.Context, config Config) (*Service, error) {
	if ctx == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.Database == nil {
		return nil, ErrUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.ProtocolVersion == 0 {
		config.ProtocolVersion = model.AgentReportProtocol
	}
	s := &Service{transfers: make(chan struct{}, 16), db: config.Database, directory: config.Directory, keys: map[string]TrustedKey{}, sigstore: config.Sigstore, pinnedOnly: config.PinnedOnly, enabled: config.Enabled, schema: config.ServerSchema, protocol: config.ProtocolVersion, now: config.Now}
	if !config.Enabled {
		return s, nil
	}
	if !validDeploymentConfig(config) {
		return nil, ErrInvalid
	}
	keys, err := configuredKeys(config.Keys)
	if err != nil {
		return nil, err
	}
	s.keys = keys
	policy, err := startupTrustPolicy(config, keys)
	if err != nil {
		return nil, err
	}
	s.signaturePolicy = policy
	if err := privateReleaseDirectory(s.directory); err != nil {
		return nil, err
	}
	if err := s.initializeTrust(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func privateReleaseDirectory(path string) error {
	if err := secureReleaseParents(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return ErrUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || !ownedReleaseFile(info) || info.Mode().Perm()&0o077 != 0 {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) Enabled() bool { return s.enabled }

// RequiresSigstoreEvidence reports whether remote release verification must fetch
// the Sigstore bundle in addition to the pinned Ed25519 signature.
func (s *Service) RequiresSigstoreEvidence() bool { return s != nil && !s.pinnedOnly }

func (s *Service) requireEnabled() error {
	if !s.enabled {
		return ErrDisabled
	}
	return nil
}

func secureReleaseParents(path string) error {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil {
			if !info.IsDir() || !trustedReleaseOwner(info) || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
				return ErrUnavailable
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return ErrUnavailable
		}
		if parent == filepath.Dir(parent) {
			return nil
		}
	}
}

func configuredKeys(input []TrustedKey) (map[string]TrustedKey, error) {
	if len(input) == 0 || len(input) > maximumTrustKeys {
		return nil, ErrInvalid
	}
	keys := map[string]TrustedKey{}
	for _, key := range input {
		if !protocol.ValidSigningKeyID(key.ID) || len(key.PublicKey) != ed25519.PublicKeySize || !key.ExpiresAt.After(key.NotBefore) {
			return nil, ErrInvalid
		}
		if _, duplicate := keys[key.ID]; duplicate {
			return nil, ErrInvalid
		}
		key.PublicKey = slices.Clone(key.PublicKey)
		keys[key.ID] = key
	}
	return keys, nil
}

func validDeploymentConfig(config Config) bool {
	if config.PinnedOnly && config.Sigstore != nil {
		return false
	}
	return config.ServerSchema >= 1 && config.ProtocolVersion >= 1 && filepath.IsAbs(config.Directory)
}

// startupTrustPolicy 按信任模式选择启动策略快照：固定公钥模式没有 Sigstore 文档可用，
// 用公钥清单本身充当信任根，其余情况仍要求完整的 Sigstore 策略。
func startupTrustPolicy(config Config, keys map[string]TrustedKey) (*signaturePolicySnapshot, error) {
	if config.PinnedOnly {
		return snapshotPinnedPolicy(keys)
	}
	return snapshotSignaturePolicy(config.Sigstore)
}
