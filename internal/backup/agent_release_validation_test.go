package backup

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasesignature"
	"github.com/beat/backend/internal/store"
)

func TestAgentReleaseSchemaRejectsPartialViewsAndFalseLegacyLedger(t *testing.T) {
	for _, kind := range []string{"legacy", "partial", "all-views", "ledger15", "ledger-view", "bad-columns", "trigger", "uppercase-tables", "uppercase-ledger"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, sqliteEntry)
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			statements := []string{}
			switch kind {
			case "uppercase-tables":
				for _, table := range archivedAgentTables {
					statements = append(statements, "CREATE TABLE "+strings.ToUpper(table.name)+"(id TEXT)")
				}
			case "uppercase-ledger":
				statements = []string{"CREATE TABLE SCHEMA_MIGRATIONS(version INTEGER)", "INSERT INTO SCHEMA_MIGRATIONS VALUES(15)"}
			case "partial":
				statements = []string{"CREATE TABLE agent_releases(id TEXT)"}
			case "all-views":
				for _, table := range archivedAgentTables {
					statements = append(statements, "CREATE VIEW "+table.name+" AS SELECT 1 AS id")
				}
			case "ledger15":
				statements = []string{"CREATE TABLE schema_migrations(version INTEGER)", "INSERT INTO schema_migrations VALUES(15)"}
			case "ledger-view":
				statements = []string{"CREATE VIEW schema_migrations AS SELECT 14 AS version"}
			case "bad-columns", "trigger":
				if err = store.MigrateAgentRollout(t.Context(), db); err != nil {
					t.Fatal(err)
				}
				if kind == "bad-columns" {
					statements = []string{"ALTER TABLE agent_release_artifacts RENAME COLUMN digest TO wrong_digest"}
				} else {
					statements = []string{"CREATE TRIGGER unsafe_release AFTER UPDATE ON agent_releases BEGIN SELECT 1; END"}
				}
			}
			for _, statement := range statements {
				if _, err = db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			cache, err := snapshotAgentReleaseCache(t.Context(), path)
			if kind == "legacy" {
				if err != nil || cache != nil {
					t.Fatal(cache, err)
				}
			} else if err == nil {
				t.Fatal("malformed schema accepted")
			}
		})
	}
}

func TestAgentReleaseRowsRejectSnapshotInconsistency(t *testing.T) {
	mutations := map[string]string{
		"artifact digest":  "UPDATE agent_release_artifacts SET digest=printf('%064d',1)",
		"artifact size":    "UPDATE agent_release_artifacts SET size=size+1",
		"missing artifact": "DELETE FROM agent_release_artifacts",
		"orphan artifact":  "UPDATE agent_release_artifacts SET release_id=printf('%064d',1)",
		"version":          "UPDATE agent_releases SET version='9.0.0'",
		"channel":          "UPDATE agent_releases SET channel='preview'",
		"expiry":           "UPDATE agent_releases SET expires_at='2020-01-01T00:00:00Z'",
		"signature":        "UPDATE agent_releases SET signature=x'00'",
		"payload":          "UPDATE agent_releases SET payload=x'7b7d'",
		"missing trust":    "DELETE FROM agent_release_trust",
		"oversize payload": "UPDATE agent_releases SET payload=zeroblob(1048577)",
	}
	for name, statement := range mutations {
		t.Run(name, func(t *testing.T) {
			root, db, _ := agentReleaseArchiveFixture(t)
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry)); err == nil {
				t.Fatal("inconsistent release accepted")
			}
		})
	}
}

func TestHistoricalExpiredRevokedReleaseRemainsArchivable(t *testing.T) {
	root, db, original := agentReleaseArchiveFixture(t)
	var payload []byte
	if err := db.QueryRow("SELECT payload FROM agent_releases").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.DecodeManifest(payload)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manifest.BuiltAt = now.Add(-48 * time.Hour)
	manifest.ExpiresAt = now.Add(-24 * time.Hour)
	manifest.Revoked = true
	payload, err = protocol.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	id := protocol.ManifestDigest(payload)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{43}, ed25519.SeedSize))
	if _, err = db.Exec("UPDATE agent_releases SET id=?,payload=?,signature=?,expires_at=?,revoked_at=?,verified=0", id, payload, ed25519.Sign(key, payload), manifest.ExpiresAt, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE agent_release_artifacts SET release_id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE agent_release_trust SET expires_at=?,revoked_at=?", now.Add(-time.Hour), now); err != nil {
		t.Fatal(err)
	}
	cache, err := snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry))
	if err != nil {
		t.Fatal(err)
	}
	original.ReleaseID = id
	if len(cache.Artifacts) != 1 || cache.Artifacts[0] != original {
		t.Fatal(cache)
	}
}

func TestAgentReleaseSnapshotRejectsUnboundedDynamicCells(t *testing.T) {
	for _, kind := range []string{"expiry", "verified", "size"} {
		t.Run(kind, func(t *testing.T) {
			root, database, _ := agentReleaseArchiveFixture(t)
			if _, err := database.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
			query := map[string]string{"expiry": "UPDATE agent_releases SET expires_at=?", "verified": "UPDATE agent_releases SET verified=?", "size": "UPDATE agent_release_artifacts SET size=?"}[kind]
			value := strings.Repeat("x", maximumManifestBytes+1)
			if _, err := database.Exec(query, value); err != nil {
				t.Fatal(err)
			}
			if _, err := snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry)); err == nil {
				t.Fatal("oversized dynamic cell accepted")
			}
		})
	}
}

func TestAgentReleaseSnapshotRejectsOversizedOpaqueCryptographicFields(t *testing.T) {
	for _, kind := range []string{"bundle", "key"} {
		t.Run(kind, func(t *testing.T) {
			root, database, _ := agentReleaseArchiveFixture(t)
			query := "UPDATE agent_releases SET sigstore_bundle=zeroblob(?)"
			if kind == "key" {
				query = "UPDATE agent_release_trust SET public_key=zeroblob(?)"
			}
			if _, err := database.Exec(query, releasesignature.MaximumBundleBytes+1); err != nil {
				t.Fatal(err)
			}
			if _, err := snapshotAgentReleaseCache(t.Context(), filepath.Join(root, sqliteEntry)); err == nil {
				t.Fatal("oversized cryptographic field accepted")
			}
		})
	}
}
