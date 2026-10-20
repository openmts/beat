package agentrolloutremote

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
)

const syncTimeout = 15 * time.Minute

func (s *Service) Sync(ctx context.Context, actor agentrollout.Actor, tag string) (agentrollout.ReleaseDetail, error) {
	if _, valid := versionFromTag(tag); !valid {
		return agentrollout.ReleaseDetail{}, ErrInvalid
	}
	if err := s.acquire(ctx); err != nil {
		return agentrollout.ReleaseDetail{}, err
	}
	defer func() { <-s.slots }()
	syncContext, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	remote, err := s.releaseByTag(syncContext, tag)
	if err != nil {
		return agentrollout.ReleaseDetail{}, err
	}
	if remote.Draft {
		return agentrollout.ReleaseDetail{}, ErrNotFound
	}
	signed, err := s.signedManifest(syncContext, remote)
	if err != nil {
		return agentrollout.ReleaseDetail{}, err
	}
	release, err := s.rollout.Import(syncContext, actor, signed)
	if err != nil {
		return agentrollout.ReleaseDetail{}, err
	}
	for _, artifact := range release.Manifest.Artifacts {
		if err := s.syncArtifact(syncContext, actor, remote, release.ID, artifact); err != nil {
			return agentrollout.ReleaseDetail{}, err
		}
	}
	return s.rollout.ReleaseDetail(ctx, actor, release.ID)
}

func (s *Service) signedManifest(ctx context.Context, release apiRelease) (protocol.SignedManifest, error) {
	payload, err := s.readNamedAsset(ctx, release, ManifestAssetName, protocol.MaximumManifestBytes)
	if err != nil {
		return protocol.SignedManifest{}, err
	}
	signature, err := s.readNamedAsset(ctx, release, SignatureAssetName, 64)
	if err != nil {
		return protocol.SignedManifest{}, err
	}
	if len(signature) != 64 {
		return protocol.SignedManifest{}, ErrInvalid
	}
	signed := protocol.SignedManifest{Payload: payload, Signature: signature}
	if !s.rollout.RequiresSigstoreEvidence() {
		return signed, nil
	}
	asset, found := namedAsset(release, SigstoreAssetName)
	if !found {
		return protocol.SignedManifest{}, ErrInvalid
	}
	asset, err = uniqueNamedAsset(release, SigstoreAssetName)
	if err != nil {
		return protocol.SignedManifest{}, err
	}
	if asset.Size <= 0 || asset.Size > 4<<20 {
		return protocol.SignedManifest{}, ErrInvalid
	}
	signed.SigstoreBundle, err = s.readAsset(ctx, asset, 4<<20)
	if err != nil {
		return protocol.SignedManifest{}, err
	}
	return signed, nil
}

func (s *Service) syncArtifact(ctx context.Context, actor agentrollout.Actor, release apiRelease, releaseID string, artifact protocol.Artifact) error {
	asset, err := uniqueNamedAsset(release, artifact.Filename)
	if err != nil || asset.Size != artifact.Size || asset.Size > protocol.MaximumArtifactBytes {
		return ErrInvalid
	}
	target, err := s.repositoryURL("releases", "assets", decimal(asset.ID))
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ErrInvalid
	}
	request.Header.Set("Accept", "application/octet-stream")
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
	if response.ContentLength > artifact.Size {
		return ErrInvalid
	}
	reader := io.LimitReader(response.Body, artifact.Size+1)
	return s.rollout.PutArtifact(ctx, actor, agentrollout.ArtifactUpload{ReleaseID: releaseID, ArtifactID: artifact.ID, Body: reader})
}

func (s *Service) readNamedAsset(ctx context.Context, release apiRelease, name string, maximum int64) ([]byte, error) {
	asset, err := uniqueNamedAsset(release, name)
	if err != nil || asset.Size <= 0 || asset.Size > maximum {
		return nil, ErrInvalid
	}
	return s.readAsset(ctx, asset, maximum)
}

func (s *Service) readAsset(ctx context.Context, asset apiAsset, maximum int64) ([]byte, error) {
	target, err := s.repositoryURL("releases", "assets", decimal(asset.ID))
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ErrInvalid
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "beat-agent-release-sync")
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, githubHTTPError(response)
	}
	if response.ContentLength > maximum || response.ContentLength > asset.Size {
		return nil, ErrInvalid
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil || int64(len(content)) > maximum || int64(len(content)) != asset.Size {
		return nil, ErrInvalid
	}
	return content, nil
}

func namedAsset(release apiRelease, name string) (apiAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return apiAsset{}, false
}

func uniqueNamedAsset(release apiRelease, name string) (apiAsset, error) {
	var found apiAsset
	count := 0
	for _, asset := range release.Assets {
		if asset.Name == name {
			found = asset
			count++
		}
	}
	if count != 1 || found.ID <= 0 || found.Size < 0 {
		return apiAsset{}, ErrInvalid
	}
	return found, nil
}

func decimal(value int64) string { return strconv.FormatInt(value, 10) }
