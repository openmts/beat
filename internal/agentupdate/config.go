package agentupdate

import (
	"crypto/ed25519"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func New(config Config) (*Updater, error) {
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.OS == "" {
		config.OS = runtime.GOOS
	}
	if config.Arch == "" {
		config.Arch = runtime.GOARCH
	}
	if config.DownloadTimeout == 0 {
		config.DownloadTimeout = 10 * time.Minute
	}
	if config.StartupTimeout == 0 {
		config.StartupTimeout = 2 * time.Minute
	}
	if config.ObservationWindow == 0 {
		config.ObservationWindow = time.Minute
	}
	if config.MaximumObservationGap == 0 {
		config.MaximumObservationGap = 30 * time.Second
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	config.Keys = cloneTrust(config.Keys)
	return &Updater{config: config}, nil
}

func validateConfig(config Config) error {
	if !validExecutablePath(config.Executable) {
		return ErrConfiguration
	}
	if !protocol.ValidSigningKeyID(config.NodeID) || config.AgentProtocol < 1 {
		return ErrConfiguration
	}
	server, err := trustedServer(config.ServerURL)
	if err != nil {
		return err
	}
	if config.Fetcher != nil {
		origin, err := trustedServer(config.Fetcher.TrustedServer())
		if err != nil || origin.String() != server.String() {
			return ErrConfiguration
		}
	}
	return validateTimeouts(config)
}

func validateTimeouts(config Config) error {
	if config.DownloadTimeout < time.Second || config.DownloadTimeout > time.Hour {
		return ErrConfiguration
	}
	if config.StartupTimeout < time.Second || config.StartupTimeout > 10*time.Minute {
		return ErrConfiguration
	}
	if config.ObservationWindow < time.Second || config.ObservationWindow > time.Hour {
		return ErrConfiguration
	}
	if config.MaximumObservationGap < time.Second || config.MaximumObservationGap > 5*time.Minute {
		return ErrConfiguration
	}
	return nil
}

func trustedServer(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrConfiguration
	}
	if strings.ContainsAny(parsed.Path, "\\\x00") || parsed.RawPath != "" {
		return nil, ErrConfiguration
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func cloneTrust(keys []protocol.SigningTrust) []protocol.SigningTrust {
	result := make([]protocol.SigningTrust, len(keys))
	for index, key := range keys {
		key.PublicKey = append(ed25519.PublicKey(nil), key.PublicKey...)
		result[index] = key
	}
	return result
}

func (u *Updater) managementGate() error {
	if u.config.Container {
		return ErrExternalManager
	}
	if u.config.OS != "linux" || (u.config.Arch != "amd64" && u.config.Arch != "arm64") {
		return ErrManualOnly
	}
	if u.config.Manager == nil || u.config.Manager.Kind() != "systemd" {
		return ErrManualOnly
	}
	if !validImage(u.config.Installed) {
		return ErrManualOnly
	}
	return nil
}

func validImage(image Image) bool {
	_, valid := protocol.CompareVersions(image.Version, image.Version)
	return valid && image.Size > 0 && image.Size <= protocol.MaximumArtifactBytes && protocol.ValidSHA256(image.SHA256)
}

func validExecutablePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && protocol.SafeFilename(filepath.Base(path))
}
