package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"beat/internal/domain"
	"beat/internal/release"
	"beat/internal/version"
)

const maxReleaseManifestSize = 1 << 20

type agentReleaseProvider interface {
	Load(context.Context) (release.Manifest, error)
}

type githubReleaseProvider struct {
	repository  string
	manifestURL string
	client      *http.Client
	ttl         time.Duration
	errorTTL    time.Duration

	mu        sync.Mutex
	manifest  release.Manifest
	fetchedAt time.Time
	lastError error
	failedAt  time.Time
}

func newGitHubReleaseProvider(repository, tag string) *githubReleaseProvider {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	tag = strings.TrimSpace(tag)
	if repository == "" || tag == "" {
		return nil
	}
	return &githubReleaseProvider{
		repository:  repository,
		manifestURL: release.ManifestURL(repository, tag),
		client: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(request *http.Request, _ []*http.Request) error {
				return release.ValidateRedirectURL(request.URL.String())
			},
		},
		ttl:      5 * time.Minute,
		errorTTL: 30 * time.Second,
	}
}

func (p *githubReleaseProvider) Load(ctx context.Context) (release.Manifest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if !p.fetchedAt.IsZero() && now.Sub(p.fetchedAt) < p.ttl {
		return p.manifest, nil
	}
	if p.lastError != nil && !p.failedAt.IsZero() && now.Sub(p.failedAt) < p.errorTTL {
		if !p.fetchedAt.IsZero() {
			return p.manifest, nil
		}
		return release.Manifest{}, p.lastError
	}
	manifest, err := p.fetch(ctx, now)
	if err != nil {
		p.lastError = err
		p.failedAt = now
		if !p.fetchedAt.IsZero() {
			return p.manifest, nil
		}
		return release.Manifest{}, err
	}
	p.manifest = manifest
	p.fetchedAt = now
	p.lastError = nil
	return manifest, nil
}

func (p *githubReleaseProvider) fetch(ctx context.Context, now time.Time) (release.Manifest, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.manifestURL, nil)
	if err != nil {
		return release.Manifest{}, fmt.Errorf("create release manifest request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return release.Manifest{}, fmt.Errorf("download release manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return release.Manifest{}, fmt.Errorf("download release manifest: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxReleaseManifestSize {
		return release.Manifest{}, errors.New("release manifest exceeds size limit")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseManifestSize+1))
	if err != nil {
		return release.Manifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	if len(body) > maxReleaseManifestSize {
		return release.Manifest{}, errors.New("release manifest exceeds size limit")
	}
	var manifest release.Manifest
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return release.Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := manifest.Validate(p.repository); err != nil {
		return release.Manifest{}, err
	}
	if manifest.ReleaseTag == "" {
		manifest.ReleaseTag = now.Format("2006-01-02")
	}
	return manifest, nil
}

type agentReleaseSnapshot struct {
	Version    string
	ReleaseTag string
	Repository string
	ReleaseURL string
	Artifacts  []domain.AgentUpgradeArtifact
	FromGitHub bool
	ResolveErr error
}

func (s *Server) agentReleaseSnapshot(ctx context.Context) agentReleaseSnapshot {
	repository := strings.Trim(strings.TrimSpace(s.cfg.AgentReleaseRepo), "/")
	if repository == "" {
		repository = version.Repository
	}
	releaseTag := strings.TrimSpace(s.cfg.AgentReleaseTag)
	if releaseTag == "" {
		releaseTag = version.ReleaseTag
	}
	if s.agentRelease != nil {
		manifest, err := s.agentRelease.Load(ctx)
		if err == nil {
			artifacts := make([]domain.AgentUpgradeArtifact, 0, 2)
			for _, arch := range []string{"amd64", "arm64"} {
				asset, assetErr := manifest.AgentAsset(arch)
				artifact := domain.AgentUpgradeArtifact{Arch: arch, Version: manifest.Version}
				if assetErr == nil {
					artifact.Size = asset.Size
					artifact.SHA256 = strings.ToLower(asset.SHA256)
					artifact.DownloadURL = asset.URL
					artifact.Available = true
				}
				artifacts = append(artifacts, artifact)
			}
			tag := manifest.ReleaseTag
			if tag == "" {
				tag = releaseTag
			}
			return agentReleaseSnapshot{
				Version: manifest.Version, ReleaseTag: tag, Repository: repository,
				ReleaseURL: release.ReleasePageURL(repository, tag), Artifacts: artifacts, FromGitHub: true,
			}
		}
		s.logger.Debug("agent_release_manifest_unavailable", "repository", repository, "tag", releaseTag, "error", err)
		return agentReleaseSnapshot{
			Version: version.Version, ReleaseTag: releaseTag, Repository: repository,
			ReleaseURL: release.ReleasePageURL(repository, releaseTag),
			Artifacts:  s.localAgentArtifacts(repository, releaseTag), ResolveErr: err,
		}
	}
	return agentReleaseSnapshot{
		Version: version.Version, ReleaseTag: releaseTag, Repository: repository,
		ReleaseURL: release.ReleasePageURL(repository, releaseTag),
		Artifacts:  s.localAgentArtifacts(repository, releaseTag),
	}
}

// localAgentArtifacts keeps development and air-gapped installations usable for
// metadata while all published download URLs still point at GitHub Releases.
func (s *Server) localAgentArtifacts(repository, releaseTag string) []domain.AgentUpgradeArtifact {
	artifacts := make([]domain.AgentUpgradeArtifact, 0, 2)
	for _, arch := range []string{"amd64", "arm64"} {
		artifact := domain.AgentUpgradeArtifact{
			Arch: arch, Version: version.Version,
			DownloadURL: release.AssetURL(repository, releaseTag, "beat-agent-linux-"+arch),
		}
		if binaryPath := s.agentBinaryPathFor(arch); binaryPath != "" {
			if info, err := os.Stat(binaryPath); err == nil && !info.IsDir() {
				artifact.Size = info.Size()
				artifact.SHA256, _ = fileSHA256(binaryPath)
				artifact.Available = artifact.SHA256 != ""
			}
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}
