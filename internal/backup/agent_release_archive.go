package backup

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

const maximumArchivedAgentReleases = 2048

const maximumArchivedAgentArtifacts = 2048

const maximumArchivedAgentProofBytes = 16 << 20

func snapshotAgentReleaseCache(ctx context.Context, path string) (cache *model.BackupAgentReleaseCache, resultErr error) {
	database, err := openArchivedSQLite(path)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	present, err := archivedAgentSchema(ctx, database)
	if err != nil || !present {
		return nil, err
	}
	if err = validateArchivedReleaseCells(ctx, database); err != nil {
		return nil, err
	}
	expected, err := archivedReleaseArtifacts(ctx, database)
	if err != nil {
		return nil, err
	}
	if err = compareArchivedArtifactRows(ctx, database, expected); err != nil {
		return nil, err
	}
	artifacts := make([]model.BackupAgentReleaseArtifact, 0, len(expected))
	for _, artifact := range expected {
		artifacts = append(artifacts, artifact)
	}
	slices.SortFunc(artifacts, compareAgentArtifact)
	return &model.BackupAgentReleaseCache{Excluded: true, RequiresVerifiedReimport: true, Artifacts: artifacts}, nil
}

func compareAgentArtifact(left, right model.BackupAgentReleaseArtifact) int {
	if left.ReleaseID < right.ReleaseID {
		return -1
	}
	if left.ReleaseID > right.ReleaseID {
		return 1
	}
	if left.ArtifactID < right.ArtifactID {
		return -1
	}
	if left.ArtifactID > right.ArtifactID {
		return 1
	}
	return 0
}

func validateAgentReleaseManifest(cache *model.BackupAgentReleaseCache) error {
	if cache == nil {
		return nil
	}
	if !validCacheExclusion(cache) {
		return errAgentReleaseArchive
	}
	for index, artifact := range cache.Artifacts {
		if !validAgentArtifactIdentity(artifact) {
			return errAgentReleaseArchive
		}
		if artifact.Size < 1 || artifact.Size > protocol.MaximumArtifactBytes {
			return errAgentReleaseArchive
		}
		if index > 0 && compareAgentArtifact(cache.Artifacts[index-1], artifact) >= 0 {
			return errAgentReleaseArchive
		}
	}
	return nil
}

func validateAgentReleaseSnapshot(ctx context.Context, root string, manifest model.BackupManifest) error {
	if err := validateAgentReleaseManifest(manifest.AgentReleaseCache); err != nil {
		return err
	}
	expected, err := snapshotAgentReleaseCache(ctx, filepath.Join(root, sqliteEntry))
	if err != nil {
		return err
	}
	actual := manifest.AgentReleaseCache
	if expected == nil && actual == nil {
		return nil
	}
	if expected == nil || actual == nil || !slices.Equal(expected.Artifacts, actual.Artifacts) {
		return errAgentReleaseArchive
	}
	return nil
}

func agentArtifactKey(artifact model.BackupAgentReleaseArtifact) string {
	return artifact.ReleaseID + "/" + artifact.ArtifactID
}

func compareArchivedArtifactRows(ctx context.Context, database *sql.DB, expected map[string]model.BackupAgentReleaseArtifact) (resultErr error) {
	const query = `SELECT CASE WHEN length(release_id)=64 THEN release_id END,CASE WHEN length(artifact_id)<=128 THEN artifact_id END,
 CASE WHEN length(digest)=64 THEN digest END,size,CASE WHEN length(state)<=7 THEN state END
 FROM agent_release_artifacts ORDER BY release_id,artifact_id LIMIT ?`
	rows, err := database.QueryContext(ctx, query, maximumArchivedAgentArtifacts+1)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	seen := map[string]bool{}
	for rows.Next() {
		if len(seen) >= maximumArchivedAgentArtifacts {
			return errAgentReleaseArchive
		}
		var artifact model.BackupAgentReleaseArtifact
		var state string
		if err = rows.Scan(&artifact.ReleaseID, &artifact.ArtifactID, &artifact.SHA256, &artifact.Size, &state); err != nil {
			return err
		}
		key := agentArtifactKey(artifact)
		if seen[key] || expected[key] != artifact || !validAgentCacheState(state) {
			return errAgentReleaseArchive
		}
		seen[key] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return errAgentReleaseArchive
	}
	return nil
}

func validAgentArtifactIdentity(artifact model.BackupAgentReleaseArtifact) bool {
	return protocol.ValidSHA256(artifact.ReleaseID) && protocol.ValidSigningKeyID(artifact.ArtifactID) && protocol.ValidSHA256(artifact.SHA256)
}

func validAgentCacheState(state string) bool { return state == "ready" || state == "missing" }

func validCacheExclusion(cache *model.BackupAgentReleaseCache) bool {
	return cache.Excluded && cache.RequiresVerifiedReimport && len(cache.Artifacts) <= maximumArchivedAgentArtifacts
}
