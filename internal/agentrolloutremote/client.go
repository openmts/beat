package agentrolloutremote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const (
	maximumAPIResponseBytes = 2 << 20
	maximumRemoteAssets     = 64
	maximumPages            = 100
	pageSize                = 20
	apiRequestTimeout       = 20 * time.Second
)

type apiRelease struct {
	ID          int64      `json:"id"`
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	PublishedAt time.Time  `json:"published_at"`
	Assets      []apiAsset `json:"assets"`
}

type apiAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (s *Service) List(ctx context.Context, page int) (Page, error) {
	if page < 1 || page > maximumPages {
		return Page{}, ErrInvalid
	}
	requestContext, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()
	target, err := s.repositoryURL("releases")
	if err != nil {
		return Page{}, err
	}
	query := target.Query()
	query.Set("per_page", strconv.Itoa(pageSize))
	query.Set("page", strconv.Itoa(page))
	target.RawQuery = query.Encode()
	var releases []apiRelease
	if err := s.readAPI(requestContext, target, &releases); err != nil {
		return Page{}, err
	}
	result := Page{Repository: s.repository, Items: make([]Release, 0, len(releases))}
	for _, release := range releases {
		result.Items = append(result.Items, s.summarizeRelease(release))
	}
	if len(releases) == pageSize {
		next := page + 1
		result.NextPage = &next
	}
	return result, nil
}

func (s *Service) releaseByTag(ctx context.Context, tag string) (apiRelease, error) {
	target, err := s.repositoryURL("releases", "tags", tag)
	if err != nil {
		return apiRelease{}, err
	}
	var release apiRelease
	if err := s.readAPI(ctx, target, &release); err != nil {
		return apiRelease{}, err
	}
	if release.ID <= 0 || release.TagName != tag || len(release.Assets) > maximumRemoteAssets {
		return apiRelease{}, ErrInvalid
	}
	return release, nil
}

func (s *Service) readAPI(ctx context.Context, target interface{ String() string }, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ErrInvalid
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "beat-agent-release-sync")
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return githubHTTPError(response)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumAPIResponseBytes+1))
	if err := decoder.Decode(output); err != nil {
		return ErrUnavailable
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrUnavailable
	}
	return nil
}

func githubHTTPError(response *http.Response) error {
	if response.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0" {
		return &RateLimitError{RetryAfter: retryAfter(response.Header.Get("Retry-After"))}
	}
	return ErrUnavailable
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return time.Minute
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}

func (s *Service) summarizeRelease(release apiRelease) Release {
	version, valid := versionFromTag(release.TagName)
	assets := make([]Asset, 0, len(release.Assets))
	for _, asset := range release.Assets {
		assets = append(assets, Asset{Name: asset.Name, Size: asset.Size})
	}
	channel := "stable"
	if release.Prerelease || strings.Contains(version, "-") {
		channel = "preview"
	}
	reason := "ready"
	if release.Draft || !valid || len(release.Assets) > maximumRemoteAssets {
		reason = "invalid"
	} else if !hasSigningAssets(release.Assets, s.rollout.RequiresSigstoreEvidence()) {
		reason = "missing_assets"
	}
	return Release{TagName: release.TagName, Version: version, Channel: channel, Name: release.Name, PublishedAt: release.PublishedAt, Prerelease: release.Prerelease, Ready: reason == "ready", Reason: reason, Assets: assets}
}

func versionFromTag(tag string) (string, bool) {
	canonical, err := protocol.ReleaseTag(tag)
	if err != nil || canonical != tag {
		return strings.TrimPrefix(tag, "v"), false
	}
	return strings.TrimPrefix(tag, "v"), true
}

func hasSigningAssets(assets []apiAsset, requireSigstore bool) bool {
	manifest, signature, sigstore := false, false, false
	for _, asset := range assets {
		switch asset.Name {
		case ManifestAssetName:
			manifest = asset.Size > 0 && asset.Size <= protocol.MaximumManifestBytes
		case SignatureAssetName:
			signature = asset.Size == 64
		case SigstoreAssetName:
			sigstore = asset.Size > 0 && asset.Size <= 4<<20
		}
	}
	return manifest && signature && (!requireSigstore || sigstore)
}
