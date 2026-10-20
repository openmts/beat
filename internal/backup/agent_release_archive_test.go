package backup

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func agentReleaseArchiveFixture(t *testing.T) (string, *sql.DB, model.BackupAgentReleaseArtifact) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, sqliteEntry)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err = store.MigrateAgentRollout(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	return root, database, seedArchivedAgentRelease(t, database)
}

func TestAgentReleaseCacheMetadataMatchesPrivateSnapshot(t *testing.T) {
	root, database, artifact := agentReleaseArchiveFixture(t)
	cache, err := snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry))
	if err != nil {
		t.Fatal(err)
	}
	if cache == nil || !cache.Excluded || !cache.RequiresVerifiedReimport || !slices.Equal(cache.Artifacts, []model.BackupAgentReleaseArtifact{artifact}) {
		t.Fatal(cache)
	}
	manifest := model.BackupManifest{FormatVersion: 4, AgentReleaseCache: cache}
	if err = validateAgentReleaseSnapshot(t.Context(), root, manifest); err != nil {
		t.Fatal(err)
	}
	cache.Artifacts[0].Size++
	if err = validateAgentReleaseSnapshot(t.Context(), root, manifest); err == nil {
		t.Fatal("mismatched manifest accepted")
	}
	if _, err = database.Exec("UPDATE agent_release_artifacts SET state='missing'"); err != nil {
		t.Fatal(err)
	}
	cache.Artifacts[0] = artifact
	if err = validateAgentReleaseSnapshot(t.Context(), root, manifest); err != nil {
		t.Fatal("missing cache should retain metadata", err)
	}
	manifest.AgentReleaseCache = nil
	if err = validateAgentReleaseSnapshot(t.Context(), root, manifest); err == nil {
		t.Fatal("v15 snapshot silently omitted cache declaration")
	}
}

func TestAgentReleaseCacheBinaryNeverEntersArchive(t *testing.T) {
	fixture := newBackupFixture(t)
	artifact := seedArchivedAgentRelease(t, fixture.sqlite.DB)
	cacheDir := filepath.Join(fixture.dataDir, "agent-release-cache")
	if err := os.Mkdir(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, artifact.SHA256), []byte("excluded-binary-cache-material"), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != model.BackupStateReady {
		t.Fatal(records, err)
	}
	archive := filepath.Join(fixture.service.root, record.Filename)
	validated, err := validateArchive(t.Context(), archive, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	if !slices.Equal(validated.manifest.AgentReleaseCache.Artifacts, []model.BackupAgentReleaseArtifact{artifact}) {
		t.Fatal(validated.manifest.AgentReleaseCache)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	for _, entry := range reader.File {
		if strings.Contains(entry.Name, "agent-release") || strings.Contains(entry.Name, artifact.SHA256) {
			t.Fatal("cache binary was packed", entry.Name)
		}
	}
}

func TestAgentReleaseCacheUsesFrozenDatabaseNotLiveRows(t *testing.T) {
	fixture := newBackupFixture(t)
	artifact := seedArchivedAgentRelease(t, fixture.sqlite.DB)
	root := t.TempDir()
	path := filepath.Join(root, sqliteEntry)
	if err := fixture.sqlite.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.sqlite.DB.Exec("UPDATE agent_release_artifacts SET digest=?", strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	cache, err := snapshotAgentReleaseCache(t.Context(), path)
	if err != nil || cache == nil || cache.Artifacts[0] != artifact {
		t.Fatal(cache, err)
	}
}

func TestAgentReleaseHeaderRejectsInvalidExclusionRecords(t *testing.T) {
	_, _, artifact := agentReleaseArchiveFixture(t)
	for _, kind := range []string{"included", "not-reimported", "release", "artifact", "digest", "size", "duplicate", "unordered"} {
		t.Run(kind, func(t *testing.T) {
			cache := &model.BackupAgentReleaseCache{Excluded: true, RequiresVerifiedReimport: true, Artifacts: []model.BackupAgentReleaseArtifact{artifact}}
			switch kind {
			case "included":
				cache.Excluded = false
			case "not-reimported":
				cache.RequiresVerifiedReimport = false
			case "release":
				cache.Artifacts[0].ReleaseID = "unknown"
			case "artifact":
				cache.Artifacts[0].ArtifactID = "../escape"
			case "digest":
				cache.Artifacts[0].SHA256 = "wrong"
			case "size":
				cache.Artifacts[0].Size = 0
			case "duplicate":
				cache.Artifacts = append(cache.Artifacts, artifact)
			case "unordered":
				other := artifact
				other.ArtifactID = "a"
				cache.Artifacts = append(cache.Artifacts, other)
			}
			if err := validateAgentReleaseManifest(cache); err == nil {
				t.Fatal("invalid exclusion accepted")
			}
		})
	}
	data, err := json.Marshal(model.BackupManifest{AgentReleaseCache: &model.BackupAgentReleaseCache{Excluded: true, RequiresVerifiedReimport: true, Artifacts: []model.BackupAgentReleaseArtifact{artifact}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"requires_verified_reimport\":true") {
		t.Fatal("restore contract absent")
	}
}
