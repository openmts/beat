package migrationstatus

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

func privateTemp(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestWriterPublishesBoundedAtomicMarker(t *testing.T) {
	directory := privateTemp(t)
	writer := NewWriter(directory, "0.2.0-test", "commit")
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	writer.SetClock(func() time.Time { return clock })
	if err := writer.Reset(); err != nil {
		t.Fatal(err)
	}
	if marker, err := Read(directory); err != nil || marker.State != StateIdle || marker.Phase != "" {
		t.Fatalf("idle marker: %+v %v", marker, err)
	}
	if err := writer.Started(24, 25); err != nil {
		t.Fatal(err)
	}
	if err := writer.Applied(25, "dashboard_collaboration"); err != nil {
		t.Fatal(err)
	}
	marker, err := Read(directory)
	if err != nil {
		t.Fatal(err)
	}
	if marker.State != StateApplying || marker.CurrentMigration != 25 || marker.CurrentSchema != 25 ||
		marker.SourceSchema != 24 || marker.TargetSchema != 25 || marker.MigrationName != "dashboard_collaboration" {
		t.Fatalf("marker: %+v", marker)
	}
	if err := writer.Failed(store.MigrationFailureApply, false); err != nil {
		t.Fatal(err)
	}
	if marker, err = Read(directory); err != nil || marker.State != StateFailed || marker.FailureCode != store.MigrationFailureApply || marker.RollbackRequired {
		t.Fatalf("failed marker: %+v %v", marker, err)
	}
	if err := writer.Completed(); err != nil {
		t.Fatal(err)
	}
	if marker, err = Read(directory); err != nil || marker.State != StateCompleted || marker.CurrentMigration != 0 ||
		marker.CurrentSchema != 25 || marker.CompletedAt.IsZero() {
		t.Fatalf("completed marker: %+v %v", marker, err)
	}
	info, err := os.Stat(filepath.Join(directory, FileName))
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() > maximumMarkerBytes {
		t.Fatalf("marker file: %+v %v", info, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != FileName {
			t.Fatalf("temporary marker left behind: %s", entry.Name())
		}
	}
}

func TestReadRejectsAbsentAndMalformedMarkers(t *testing.T) {
	directory := privateTemp(t)
	if _, err := Read(directory); !errors.Is(err, ErrMarkerAbsent) {
		t.Fatal("absent marker accepted")
	}
	path := filepath.Join(directory, FileName)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(directory); err == nil {
		t.Fatal("directory marker accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"unknown state":  `{"state":"unknown"}`,
		"trailing value": `{"state":"completed"} {"state":"idle"}`,
		"oversized name": `{"state":"applying","migration_name":"` + string(make([]byte, maximumNameBytes+1)) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(directory); err == nil {
				t.Fatal("malformed marker accepted")
			}
		})
	}
	oversized := make([]byte, maximumMarkerBytes+1)
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(directory); err == nil {
		t.Fatal("oversized marker accepted")
	}
}

func TestValidateRejectsOutOfRangeVersions(t *testing.T) {
	for _, marker := range []Marker{
		{State: StateChecking, CurrentSchema: -1},
		{State: StateApplying, TargetSchema: maximumVersion + 1},
		{State: StateFailed, Phase: string(make([]byte, maximumCodeBytes+1))},
	} {
		if err := marker.validate(); err == nil {
			t.Fatalf("invalid marker accepted: %+v", marker)
		}
	}
}

func TestWriterReportsFailuresForMissingDirectory(t *testing.T) {
	writer := NewWriter(filepath.Join(privateTemp(t), "absent"), "0.2.0-test", "commit")
	if err := writer.Reset(); err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestObserverForwardsStorePhases(t *testing.T) {
	directory := privateTemp(t)
	observer := NewObserver(NewWriter(directory, "0.2.0-test", "commit"))
	observer.MigrationStarted(store.MaximumSupportedSchemaVersion - 1)
	observer.MigrationApplied(store.Migration{Version: store.MaximumSupportedSchemaVersion, Name: "dashboard_collaboration"}, store.MaximumSupportedSchemaVersion)
	observer.MigrationFailed(store.MigrationFailureFutureSchema, true)
	observer.MigrationCompleted(store.MaximumSupportedSchemaVersion)
	marker, err := Read(directory)
	if err != nil {
		t.Fatal(err)
	}
	if marker.State != StateCompleted || marker.SourceSchema != store.MaximumSupportedSchemaVersion-1 ||
		marker.TargetSchema != store.MaximumSupportedSchemaVersion || marker.CurrentMigration != 0 {
		t.Fatalf("marker: %+v", marker)
	}
}
