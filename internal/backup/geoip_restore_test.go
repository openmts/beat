package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestGeoIPInvalidArchiveFailsBeforeAnyReplacement(t *testing.T) {
	fixture := newBackupFixture(t)
	configureGeoIPBackup(t, fixture)
	raw, _ := generatedArchive(t, fixture)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "backups", "invalid.zip")
	if err := os.WriteFile(archive, dropZipEntry(t, raw, geoIPCurrentEntry), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(filepath.Join(root, "restore.pending.json"), restoreJournal("geo", archive)); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "beat.db")
	writeTestFile(t, database, "original database")
	applier := NewApplier()
	swaps := 0
	applier.rename = func(source, target string) error { swaps++; return os.Rename(source, target) }
	if err := applier.ApplyPending(t.Context(), root, database, filepath.Join(root, "mts")); err == nil {
		t.Fatal("invalid MMDB archive applied")
	}
	if swaps != 0 {
		t.Fatal("validation failure replaced files", swaps)
	}
	assertFileContent(t, database, "original database")
}

func TestGeoIPRestoreClearsStaleAssetsAndCanRollback(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "geoip")
	if err := os.Mkdir(live, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"country.mmdb", "country.mmdb.previous", ".publication.json"} {
		writeTestFile(t, filepath.Join(live, name), "old")
	}
	prepared := &preparedRestore{}
	if err := prepareGeoIPRestore(&validatedArchive{root: t.TempDir()}, prepared, geoIPRestoreTarget{dataDir: root, token: "empty"}); err != nil {
		t.Fatal(err)
	}
	defer prepared.cleanup()
	applier := NewApplier()
	old, err := applier.swap(prepared, "empty")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(live)
	if err != nil || len(entries) != 0 {
		t.Fatal("stale GeoIP assets survived", entries, err)
	}
	if err := applier.undoSwap(prepared.targets, prepared.targets, old); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(live, ".publication.json"), "old")
}

func TestGeoIPReconcileLeasesPreservesManualAndEvents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE node_locations(id TEXT,mode TEXT,lease_hash TEXT,lease_expires_at DATETIME,worker_id TEXT,next_refresh_at DATETIME,country_code TEXT);
 CREATE TABLE node_lifecycle_events(id TEXT PRIMARY KEY);
 INSERT INTO node_lifecycle_events VALUES('unchanged');`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, mode := range []string{"observed", "managed_target", "manual", "disabled"} {
		if _, err = db.Exec(`INSERT INTO node_locations VALUES(?,?,?,?,'worker',?,'US')`, mode, mode, "lease", now.Add(time.Hour), now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := reconcileGeoIPRestore(t.Context(), path, now); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"observed", "managed_target", "manual", "disabled"} {
		var lease, country string
		if err := db.QueryRow("SELECT lease_hash,country_code FROM node_locations WHERE id=?", mode).Scan(&lease, &country); err != nil {
			t.Fatal(err)
		}
		expected := "lease"
		if mode == "observed" || mode == "managed_target" {
			expected = ""
		}
		if lease != expected || country != "US" {
			t.Fatal(mode, lease, country)
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM node_lifecycle_events").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := reconcileGeoIPRestore(t.Context(), filepath.Join(root, "missing"), now); err == nil {
		t.Fatal("missing DB accepted")
	}
}

func TestGeoIPRestorePreparationFailures(t *testing.T) {
	for _, kind := range []string{"existing", "unknown", "missing"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			prepared := &preparedRestore{}
			defer prepared.cleanup()
			validated := &validatedArchive{root: t.TempDir(), manifest: model.BackupManifest{GeoIPFiles: map[string]model.MMDBMetadata{}}}
			switch kind {
			case "existing":
				writeTestFile(t, filepath.Join(root, "geoip.restore-new-token"), "exists")
			case "unknown":
				validated.manifest.GeoIPFiles["geoip/escape"] = model.MMDBMetadata{}
			case "missing":
				validated.manifest.GeoIPFiles[geoIPCurrentEntry] = model.MMDBMetadata{}
			}
			if err := prepareGeoIPRestore(validated, prepared, geoIPRestoreTarget{dataDir: root, token: "token"}); err == nil {
				t.Fatal("invalid preparation accepted")
			}
		})
	}
}
