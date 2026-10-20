package backup

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

func TestAgentReleaseSnapshotQueriesRespectCancellation(t *testing.T) {
	_, database, _ := agentReleaseArchiveFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	checks := []func() error{
		func() error { _, err := archivedAgentSchema(ctx, database); return err },
		func() error { return validateLegacyAgentSchema(ctx, database) },
		func() error { return validateAgentTable(ctx, database, archivedAgentTables[0]) },
		func() error { return validateArchivedReleaseCells(ctx, database) },
		func() error { return boundedAgentRows(ctx, database) },
		func() error { _, err := archivedReleaseArtifacts(ctx, database); return err },
		func() error { return compareArchivedArtifactRows(ctx, database, nil) },
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled snapshot query continued", err)
		}
	}
}

func TestAgentReleaseExclusionSortingAndCollectionBounds(t *testing.T) {
	_, _, artifact := agentReleaseArchiveFixture(t)
	lower := artifact
	lower.ReleaseID = strings.Repeat("0", 64)
	lower.ArtifactID = "a"
	upper := artifact
	upper.ReleaseID = strings.Repeat("f", 64)
	upper.ArtifactID = "z"
	sameRelease := lower
	sameRelease.ArtifactID = "z"
	entries := []model.BackupAgentReleaseArtifact{upper, sameRelease, lower}
	slices.SortFunc(entries, compareAgentArtifact)
	if entries[0] != lower || entries[1] != sameRelease || entries[2] != upper {
		t.Fatal(entries)
	}
	cache := &model.BackupAgentReleaseCache{Excluded: true, RequiresVerifiedReimport: true, Artifacts: entries}
	if err := validateAgentReleaseManifest(cache); err != nil {
		t.Fatal(err)
	}
	cache.Artifacts = make([]model.BackupAgentReleaseArtifact, maximumArchivedAgentArtifacts+1)
	if err := validateAgentReleaseManifest(cache); err == nil {
		t.Fatal("oversized exclusion set accepted")
	}
	if err := validateManifestVersion(model.BackupManifest{FormatVersion: 3, AgentReleaseCache: cache}); err == nil {
		t.Fatal("legacy format accepted v15 metadata")
	}
}

func TestArtifactRowsAreBoundedEvenWithOneRelease(t *testing.T) {
	root, database, artifact := agentReleaseArchiveFixture(t)
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maximumArchivedAgentArtifacts; index++ {
		_, err = tx.Exec("INSERT INTO agent_release_artifacts(release_id,artifact_id,digest,size,state,updated_at) VALUES(?,?,?,?,'missing',?)", artifact.ReleaseID, fmt.Sprintf("extra-%d", index), artifact.SHA256, artifact.Size, time.Now())
		if err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry)); err == nil {
		t.Fatal("artifact table scan was not bounded")
	}
}

func TestEmptyV15CacheAndInvalidSnapshotDeclarations(t *testing.T) {
	root, database, _ := agentReleaseArchiveFixture(t)
	if _, err := database.Exec("DELETE FROM agent_release_artifacts"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM agent_releases"); err != nil {
		t.Fatal(err)
	}
	cache, err := snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry))
	if err != nil || cache == nil || len(cache.Artifacts) != 0 {
		t.Fatal(cache, err)
	}
	manifest := model.BackupManifest{FormatVersion: 4, AgentReleaseCache: cache}
	if err = validateAgentReleaseSnapshot(t.Context(), root, manifest); err != nil {
		t.Fatal(err)
	}
	cache.Excluded = false
	if err = validateAgentReleaseSnapshot(t.Context(), root, manifest); err == nil {
		t.Fatal("included cache claim accepted")
	}
	cache.Excluded = true
	if err = validateAgentReleaseSnapshot(t.Context(), t.TempDir(), manifest); err == nil {
		t.Fatal("missing snapshot accepted")
	}
}

func TestArchivedTransitionStructureDoesNotReestablishTrust(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour)
	next := &protocol.KeyTransition{ID: "archived-next", PublicKey: make([]byte, ed25519.PublicKeySize), Signature: make([]byte, ed25519.SignatureSize), NotBefore: at.Add(-time.Hour), ExpiresAt: at}
	if !validArchivedTransition(next) {
		t.Fatal("expired transition structure discarded")
	}
	next.PublicKey = []byte("oversized or invalid")
	if validArchivedTransition(next) {
		t.Fatal("invalid transition shape accepted")
	}
}
