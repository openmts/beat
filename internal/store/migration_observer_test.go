package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

type recordingObserver struct {
	code        string
	applied     []int
	started     int
	completed   int
	failed      bool
	rollbackReq bool
}

func (observer *recordingObserver) MigrationStarted(source int) { observer.started = source }

func (observer *recordingObserver) MigrationApplied(migration Migration, current int) {
	observer.applied = append(observer.applied, current)
}

func (observer *recordingObserver) MigrationFailed(code string, rollbackRequired bool) {
	observer.failed, observer.code, observer.rollbackReq = true, code, rollbackRequired
}

func (observer *recordingObserver) MigrationCompleted(current int) { observer.completed = current }

func TestObservedMigrationReportsEveryPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "beat.db")
	observer := &recordingObserver{}
	database, err := NewSQLiteStoreObserved(context.Background(), path, observer)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if observer.failed || observer.started != 0 || observer.completed != MaximumSupportedSchemaVersion {
		t.Fatalf("observer: %+v", observer)
	}
	inventory := MigrationInventory()
	if len(observer.applied) != len(inventory) || observer.applied[len(observer.applied)-1] != MaximumSupportedSchemaVersion {
		t.Fatalf("applied: %+v", observer.applied)
	}
}

func TestObservedMigrationReportsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "beat.db")
	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL);
		INSERT INTO schema_migrations (version, name, applied_at) VALUES (99, 'future', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	observer := &recordingObserver{}
	if _, err := NewSQLiteStoreObserved(context.Background(), path, observer); err == nil {
		t.Fatal("future schema accepted")
	}
	if !observer.failed || observer.code != MigrationFailureFutureSchema || !observer.rollbackReq || observer.completed != 0 {
		t.Fatalf("observer: %+v", observer)
	}
}

func TestMigrationFailureCodesAreBoundedEnums(t *testing.T) {
	for name, input := range map[string]error{
		"nil":        nil,
		"permission": os.ErrPermission,
		"wrapped":    migrationFail(MigrationFailureLedger, errors.New("ledger")),
		"lock":       migrationFail(MigrationFailureBusy, errors.New("database is locked")),
		"unknown":    errors.New("something else"),
	} {
		t.Run(name, func(t *testing.T) {
			code := MigrationFailureCode(input)
			switch code {
			case "", MigrationFailureFutureSchema, MigrationFailureLedger, MigrationFailureApply,
				MigrationFailureBusy, MigrationFailurePermission, MigrationFailureUnknown:
			default:
				t.Fatalf("unbounded code %q", code)
			}
			if name == "nil" && code != "" {
				t.Fatalf("nil error produced code %q", code)
			}
			if name == "permission" && code != MigrationFailurePermission {
				t.Fatalf("permission classified as %q", code)
			}
		})
	}
	if code := MigrationFailureCode(unix.EACCES); code == "" {
		t.Fatal("raw permission error produced no code")
	}
}

func TestMigrationInventoryIsConsecutiveAndCopyable(t *testing.T) {
	inventory := MigrationInventory()
	if len(inventory) != MaximumSupportedSchemaVersion {
		t.Fatalf("inventory: %d", len(inventory))
	}
	for index, migration := range inventory {
		if migration.Version != index+1 || migration.Name == "" {
			t.Fatalf("inventory entry: %+v", migration)
		}
	}
	pending := PendingMigrations(MaximumSupportedSchemaVersion - 1)
	if len(pending) != 1 || pending[0].Version != MaximumSupportedSchemaVersion || pending[0].Name != inventory[len(inventory)-1].Name {
		t.Fatalf("pending: %+v", pending)
	}
	if len(PendingMigrations(MaximumSupportedSchemaVersion)) != 0 || len(PendingMigrations(0)) != len(inventory) {
		t.Fatal("pending bounds are wrong")
	}
	inventory[0].Name = "mutated"
	if MigrationInventory()[0].Name == "mutated" {
		t.Fatal("inventory shares mutable state")
	}
}
