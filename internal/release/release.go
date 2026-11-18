package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

const ManifestName = "beat-manifest.json"

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type AgentAssets struct {
	AMD64 Asset `json:"amd64"`
	ARM64 Asset `json:"arm64"`
}

type ServerAssets struct {
	AMD64 Asset `json:"amd64"`
	ARM64 Asset `json:"arm64"`
}

type Assets struct {
	Agent  AgentAssets  `json:"agent"`
	Server ServerAssets `json:"server"`
}

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	ReleaseTag    string `json:"releaseTag"`
	Repository    string `json:"repository"`
	Commit        string `json:"commit"`
	PublishedAt   string `json:"publishedAt"`
	Assets        Assets `json:"assets"`
}

func (m Manifest) AgentAsset(arch string) (Asset, error) {
	switch arch {
	case "amd64":
		return m.Assets.Agent.AMD64, nil
	case "arm64":
		return m.Assets.Agent.ARM64, nil
	default:
		return Asset{}, fmt.Errorf("unsupported Agent architecture %q", arch)
	}
}

func (m Manifest) Validate(repository string) error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("unsupported release manifest schema version %d", m.SchemaVersion)
	}
	if strings.TrimSpace(m.Version) == "" || strings.TrimSpace(m.ReleaseTag) == "" {
		return errors.New("release manifest version and releaseTag are required")
	}
	repository = strings.TrimSpace(repository)
	if m.Repository != "" && repository != "" && !strings.EqualFold(m.Repository, repository) {
		return fmt.Errorf("release manifest repository %q does not match %q", m.Repository, repository)
	}
	for arch, asset := range map[string]Asset{"amd64": m.Assets.Agent.AMD64, "arm64": m.Assets.Agent.ARM64} {
		expectedName := "beat-agent-linux-" + arch
		if asset.Name != expectedName {
			return fmt.Errorf("release manifest Agent asset for %s has name %q, want %q", arch, asset.Name, expectedName)
		}
		if err := ValidateAgentDownloadURL(asset.URL, repository, arch); err != nil {
			return fmt.Errorf("release manifest Agent asset for %s: %w", arch, err)
		}
		if asset.Size <= 0 {
			return fmt.Errorf("release manifest Agent asset for %s has invalid size", arch)
		}
		digest := strings.ToLower(strings.TrimSpace(asset.SHA256))
		if len(digest) != sha256.Size*2 {
			return fmt.Errorf("release manifest Agent asset for %s has invalid SHA-256", arch)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return fmt.Errorf("release manifest Agent asset for %s has invalid SHA-256", arch)
		}
	}
	return nil
}

func ReleasePageURL(repository, tag string) string {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if tag = strings.TrimSpace(tag); tag == "" {
		tag = "latest"
	}
	if tag == "latest" {
		return fmt.Sprintf("https://github.com/%s/releases/latest", repository)
	}
	return fmt.Sprintf("https://github.com/%s/releases/tag/%s", repository, url.PathEscape(tag))
}

func AssetURL(repository, tag, name string) string {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	tag = strings.TrimSpace(tag)
	if tag == "" {
		tag = "latest"
	}
	if tag == "latest" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repository, url.PathEscape(name))
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repository, url.PathEscape(tag), url.PathEscape(name))
}

func ManifestURL(repository, tag string) string {
	return AssetURL(repository, tag, ManifestName)
}

func ValidateAgentDownloadURL(raw, repository, arch string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("invalid Agent download URL")
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Agent download URL must be an HTTPS GitHub release asset")
	}
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return errors.New("release repository is required")
	}
	releasePrefix := "/" + repository + "/releases/"
	releasePath := strings.TrimPrefix(parsed.Path, releasePrefix)
	if releasePath == parsed.Path || releasePath == "" {
		return errors.New("Agent download URL is outside the configured GitHub repository")
	}
	assetName := ""
	switch {
	case strings.HasPrefix(releasePath, "latest/download/"):
		assetName = strings.TrimPrefix(releasePath, "latest/download/")
	case strings.HasPrefix(releasePath, "download/"):
		parts := strings.Split(strings.TrimPrefix(releasePath, "download/"), "/")
		if len(parts) != 2 || parts[0] == "" {
			return errors.New("Agent download URL is not a GitHub release tag asset")
		}
		assetName = parts[1]
	default:
		return errors.New("Agent download URL is not a GitHub release asset")
	}
	expectedName := "beat-agent-linux-" + arch
	if path.Base(assetName) != expectedName || strings.Contains(assetName, "/") {
		return errors.New("Agent download URL architecture does not match the upgrade target")
	}
	return nil
}

func ValidateRedirectURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("invalid download redirect URL")
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" {
		return errors.New("download redirect must use HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "github.com" || host == "githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("download redirect host %q is not allowed", host)
}

func ExpectedSHA256(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
