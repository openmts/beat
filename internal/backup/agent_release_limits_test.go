package backup

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestBackupManifestWriterAndReaderShareExactByteLimit(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	manifest := model.BackupManifest{FormatVersion: 4, CatalogVersion: metriccatalog.Version, CatalogRevision: 1, CreatedAt: now, SnapshotCutoff: now, Checksums: map[string]string{}, PayloadSizes: map[string]int64{}}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifest.BeatVersion = strings.Repeat("x", maximumManifestBytes-len(encoded)-1)
	if err = writeMetadataFiles(root, manifest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, manifestEntry))
	if err != nil || info.Size() != maximumManifestBytes {
		t.Fatal(info, err)
	}
	if _, err = readManifest(filepath.Join(root, manifestEntry)); err != nil {
		t.Fatal("writer generated unreadable boundary manifest", err)
	}
	manifest.BeatVersion += "x"
	if err = writeMetadataFiles(root, manifest); err == nil {
		t.Fatal("oversized manifest published")
	}
	info, err = os.Stat(filepath.Join(root, manifestEntry))
	if err != nil || info.Size() != maximumManifestBytes {
		t.Fatal("valid manifest was overwritten", err)
	}
}

func TestOversizedManifestCannotBecomeReadyArchive(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.service.version = strings.Repeat("x", maximumManifestBytes)
	_, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != model.BackupStateFailed {
		t.Fatal(records, err)
	}
	entries, err := os.ReadDir(fixture.service.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("oversized manifest left a ready or partial archive", entries)
	}
}

func TestArchiveRejectsOversizedManifestBeforeExtraction(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "oversized.zip")
	writeZipEntries(t, archive, []zipTestEntry{{name: manifestEntry, content: strings.Repeat("x", maximumManifestBytes+1)}, {name: sqliteEntry, content: "db"}, {name: metricsEntry, content: "metrics"}, {name: checksumsEntry, content: "checksums"}})
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	output := t.TempDir()
	if err = extractAllowlisted(reader.File, output); err == nil {
		t.Fatal("oversized manifest extracted")
	}
	if _, err = os.Stat(filepath.Join(output, manifestEntry)); !os.IsNotExist(err) {
		t.Fatal("oversized manifest was materialized", err)
	}
}

func TestAgentReleaseCollectionLimitIsEnforcedOnSnapshot(t *testing.T) {
	root, database, _ := agentReleaseArchiveFixture(t)
	var payload []byte
	if err := database.QueryRow("SELECT payload FROM agent_releases").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.DecodeManifest(payload)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{43}, ed25519.SeedSize))
	for index := 1; index <= maximumArchivedAgentReleases; index++ {
		manifest.Version = fmt.Sprintf("2.0.%d", index)
		if err = insertArchiveLimitRelease(tx, manifest, key); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry)); err == nil {
		t.Fatal("unbounded release set accepted")
	}
}

func insertArchiveLimitRelease(tx *sql.Tx, manifest protocol.Manifest, key ed25519.PrivateKey) error {
	payload, err := protocol.CanonicalManifest(manifest)
	if err != nil {
		return err
	}
	id := protocol.ManifestDigest(payload)
	_, err = tx.Exec(`INSERT INTO agent_releases(id,version,channel,signing_key_id,payload,signature,sigstore_bundle,expires_at,signature_policy_revision,verified,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,1,?,?)`,
		id, manifest.Version, manifest.Channel, manifest.SigningKeyID, payload, ed25519.Sign(key, payload), []byte("opaque"), manifest.ExpiresAt, "archive-policy", "archive-owner", manifest.BuiltAt)
	if err != nil {
		return err
	}
	artifact := manifest.Artifacts[0]
	_, err = tx.Exec(`INSERT INTO agent_release_artifacts(release_id,artifact_id,digest,size,state,updated_at) VALUES(?,?,?,?,'missing',?)`, id, artifact.ID, artifact.SHA256, artifact.Size, manifest.BuiltAt)
	return err
}
