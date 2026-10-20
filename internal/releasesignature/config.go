// Package releasesignature verifies offline Sigstore evidence against pinned trust.
package releasesignature

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	MaximumPayloadBytes  = 1 << 20
	MaximumBundleBytes   = 4 << 20
	maximumRootBytes     = 1 << 20
	maximumIdentityBytes = 2048
	maximumOutputBytes   = 64 << 10
	maximumConcurrent    = 2
	maximumDuration      = 30 * time.Second
	defaultDuration      = 10 * time.Second
)

var (
	ErrConfiguration = errors.New("invalid release signature trust configuration")
	ErrRejected      = errors.New("release signature verification rejected")
	ErrUnavailable   = errors.New("release signature verifier is unavailable")
	ErrOutputLimit   = errors.New("release signature verifier exceeded output limit")
	ErrBusy          = errors.New("release signature verifier is busy")
)

type Evidence struct {
	Payload, Bundle []byte
	ReleaseTag      string
}

func (Evidence) String() string { return "SignatureEvidence{redacted}" }

func (e Evidence) GoString() string { return e.String() }

// Config 仅来自启动时安装的信任配置，不能从发布上传请求中构造。
type Config struct {
	Executable, CertificateIdentity, OIDCIssuer, TemporaryDirectory string
	ReleaseWorkflow                                                 string
	TrustedRoot                                                     []byte
	Timeout                                                         time.Duration
}

type Verifier struct {
	config           Config
	executable       os.FileInfo
	executableDigest string
	slots            chan struct{}
}

func New(config Config) (*Verifier, error) {
	config, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	info, err := trustedExecutable(config.Executable)
	if err != nil {
		return nil, err
	}
	digest, err := verifierDigest(config.Executable, info)
	if err != nil {
		return nil, err
	}
	config.TrustedRoot = slices.Clone(config.TrustedRoot)
	return &Verifier{config: config, executable: info, executableDigest: digest, slots: make(chan struct{}, maximumConcurrent)}, nil
}

func normalizeConfig(config Config) (Config, error) {
	if !validIdentityPolicy(config) || !validIssuer(config.OIDCIssuer) {
		return config, ErrConfiguration
	}
	if len(config.TrustedRoot) == 0 || len(config.TrustedRoot) > maximumRootBytes || !json.Valid(config.TrustedRoot) {
		return config, ErrConfiguration
	}
	return normalizeRuntimeConfig(config)
}

func normalizeRuntimeConfig(config Config) (Config, error) {
	if config.Timeout == 0 {
		config.Timeout = defaultDuration
	}
	if config.Timeout < time.Millisecond || config.Timeout > maximumDuration {
		return config, ErrConfiguration
	}
	if config.TemporaryDirectory == "" {
		config.TemporaryDirectory = os.TempDir()
	}
	if !filepath.IsAbs(config.TemporaryDirectory) || filepath.Clean(config.TemporaryDirectory) != config.TemporaryDirectory {
		return config, ErrConfiguration
	}
	if !trustedDirectoryChain(config.TemporaryDirectory) {
		return config, ErrConfiguration
	}
	return config, nil
}

func trustedExecutable(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrConfiguration
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !trustedOwner(info) || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return nil, ErrConfiguration
	}
	if !trustedDirectoryChain(filepath.Dir(path)) {
		return nil, ErrConfiguration
	}
	return info, nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maximumIdentityBytes && !strings.ContainsAny(value, "\x00\r\n")
}

func validIssuer(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && validIdentity(value) && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}
