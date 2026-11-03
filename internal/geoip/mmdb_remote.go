package geoip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	githubMMDBRepository = "P3TERX/GeoLite.mmdb"
	githubMMDBAsset      = "GeoLite2-Country.mmdb"
	githubMMDBAPI        = "https://api.github.com/repos/" + githubMMDBRepository + "/releases"
	githubMMDBDownload   = "https://github.com/" + githubMMDBRepository + "/releases/download"
	githubMetadataLimit  = 64 * 1024
	githubDownloadLimit  = 60 * time.Second
	githubRedirectLimit  = 4
)

var githubVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type RemoteMMDBRelease struct {
	Repository      string    `json:"repository"`
	Version         string    `json:"version"`
	PublishedAt     time.Time `json:"published_at"`
	AssetName       string    `json:"asset_name"`
	SizeBytes       int64     `json:"size_bytes"`
	SHA256          string    `json:"sha256"`
	UpdateAvailable bool      `json:"update_available"`
}

type RemoteMMDBInstall struct {
	Version          string `json:"version"`
	SHA256           string `json:"sha256"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func (s *Service) RemoteMMDBRelease(ctx context.Context, actor Actor) (RemoteMMDBRelease, error) {
	current, err := s.Settings(ctx, actor)
	if err != nil {
		return RemoteMMDBRelease{}, err
	}
	release, err := s.fetchGitHubRelease(ctx, "")
	if err != nil {
		return RemoteMMDBRelease{}, err
	}
	release.UpdateAvailable = !strings.EqualFold(current.MMDB.SHA256, release.SHA256)
	return release, nil
}

func (s *Service) InstallRemoteMMDB(ctx context.Context, actor Actor, input RemoteMMDBInstall) (model.GeoIPSettings, error) {
	if !validRemoteInstall(input) {
		return model.GeoIPSettings{}, ErrInvalid
	}
	if s.external != "" {
		return model.GeoIPSettings{}, ErrForbidden
	}
	if err := s.write(ctx, func(tx transaction) error { return tx.authorize(actor, true, true) }); err != nil {
		return model.GeoIPSettings{}, err
	}
	release, err := s.fetchGitHubRelease(ctx, input.Version)
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	if release.Version != input.Version || !strings.EqualFold(release.SHA256, input.SHA256) {
		return model.GeoIPSettings{}, ErrConflict
	}
	path, err := s.downloadGitHubMMDB(ctx, release)
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	defer func() { _ = os.Remove(path) }()
	file, err := os.Open(path)
	if err != nil {
		return model.GeoIPSettings{}, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	source := fmt.Sprintf("github:%s@%s", githubMMDBRepository, release.Version)
	return s.InstallMMDB(ctx, actor, MMDBUpload{Body: file, ExpectedRevision: input.ExpectedRevision, source: source})
}

func validRemoteInstall(input RemoteMMDBInstall) bool {
	_, err := hex.DecodeString(input.SHA256)
	return input.ExpectedRevision > 0 && githubVersionPattern.MatchString(input.Version) && err == nil && len(input.SHA256) == sha256.Size*2
}

func (s *Service) fetchGitHubRelease(ctx context.Context, version string) (RemoteMMDBRelease, error) {
	endpoint := githubMMDBAPI + "/latest"
	if version != "" {
		if !githubVersionPattern.MatchString(version) {
			return RemoteMMDBRelease{}, ErrInvalid
		}
		endpoint = githubMMDBAPI + "/tags/" + url.PathEscape(version)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RemoteMMDBRelease{}, ErrUnavailable
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "beat-geoip")
	response, err := s.client.Do(request)
	if err != nil {
		return RemoteMMDBRelease{}, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return RemoteMMDBRelease{}, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, githubMetadataLimit+1))
	if err != nil || len(body) > githubMetadataLimit {
		return RemoteMMDBRelease{}, ErrUnavailable
	}
	return decodeGitHubRelease(body)
}

func decodeGitHubRelease(body []byte) (RemoteMMDBRelease, error) {
	var payload githubRelease
	if err := json.Unmarshal(body, &payload); err != nil || !githubVersionPattern.MatchString(payload.TagName) {
		return RemoteMMDBRelease{}, ErrInvalid
	}
	published, err := time.Parse(time.RFC3339, payload.PublishedAt)
	if err != nil {
		return RemoteMMDBRelease{}, ErrInvalid
	}
	for _, asset := range payload.Assets {
		if asset.Name != githubMMDBAsset {
			continue
		}
		digest := strings.TrimPrefix(asset.Digest, "sha256:")
		decoded, decodeErr := hex.DecodeString(digest)
		if asset.Size <= 0 || asset.Size > MaximumMMDBBytes || decodeErr != nil || len(decoded) != sha256.Size {
			return RemoteMMDBRelease{}, ErrInvalid
		}
		return RemoteMMDBRelease{
			Repository:  githubMMDBRepository,
			Version:     payload.TagName,
			PublishedAt: published.UTC(),
			AssetName:   githubMMDBAsset,
			SizeBytes:   asset.Size,
			SHA256:      strings.ToLower(digest),
		}, nil
	}
	return RemoteMMDBRelease{}, ErrNotFound
}

func (s *Service) downloadGitHubMMDB(ctx context.Context, release RemoteMMDBRelease) (string, error) {
	endpoint := githubMMDBDownload + "/" + url.PathEscape(release.Version) + "/" + githubMMDBAsset
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", ErrUnavailable
	}
	client := githubDownloadClient(s.client)
	response, err := client.Do(request)
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || response.ContentLength > MaximumMMDBBytes {
		return "", ErrInvalid
	}
	hash := sha256.New()
	path, err := s.stageDatabase(io.TeeReader(response.Body, hash))
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(release.SHA256, hex.EncodeToString(hash.Sum(nil))) {
		_ = os.Remove(path)
		return "", ErrInvalid
	}
	return path, nil
}

func githubDownloadClient(base *http.Client) *http.Client {
	client := *base
	client.Timeout = githubDownloadLimit
	client.CheckRedirect = allowedGitHubRedirect
	if transport, ok := base.Transport.(*http.Transport); ok {
		transport = transport.Clone()
		transport.ResponseHeaderTimeout = githubDownloadLimit
		client.Transport = transport
	}
	return &client
}

func allowedGitHubRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= githubRedirectLimit || !trustedGitHubRedirect(request.URL) {
		return http.ErrUseLastResponse
	}
	return nil
}

func trustedGitHubRedirect(target *url.URL) bool {
	if target == nil || target.Scheme != "https" || target.User != nil || target.Port() != "" && target.Port() != "443" {
		return false
	}
	switch strings.ToLower(target.Hostname()) {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	default:
		return false
	}
}
