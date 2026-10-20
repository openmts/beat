package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectArchiveReportsPreflightAndCleansStaging(t *testing.T) {
	fixture := newBackupFixture(t)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	stored, err := fixture.service.record(t.Context(), record.ID)
	if err != nil || stored == nil {
		t.Fatalf("read backup record: %v", err)
	}
	archive := filepath.Join(fixture.service.root, stored.Filename)
	staging := t.TempDir()
	preflight, err := InspectArchive(t.Context(), archive, staging)
	if err != nil {
		t.Fatalf("inspect archive: %v", err)
	}
	if preflight.FormatVersion <= 0 || preflight.BeatVersion == "" || preflight.root == "" {
		t.Fatalf("preflight = %+v", preflight)
	}
	if preflight.DatabasePath != filepath.Join(preflight.root, sqliteEntry) {
		t.Fatalf("database path = %s", preflight.DatabasePath)
	}
	if info, err := os.Stat(preflight.DatabasePath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("unpacked database = %v %v", info, err)
	}
	root := preflight.root
	if err := preflight.Close(); err != nil {
		t.Fatalf("close preflight: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("staging root survived close: %v", err)
	}
	if err := preflight.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
	var nilPreflight *ArchivePreflight
	if err := nilPreflight.Close(); err != nil {
		t.Fatalf("nil close = %v", err)
	}
	if _, err := InspectArchive(t.Context(), filepath.Join(staging, "absent.zip"), staging); err == nil {
		t.Fatal("missing archive was accepted")
	}
}
