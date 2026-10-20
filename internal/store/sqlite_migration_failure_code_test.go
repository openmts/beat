package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationFailureClassification(t *testing.T) {
	sentinel := errors.New("cov sentinel")
	wrapped := migrationFail(MigrationFailureLedger, sentinel)
	if !errors.Is(wrapped, sentinel) {
		t.Fatal("migration failure must unwrap to its cause")
	}
	if MigrationFailureCode(wrapped) != MigrationFailureLedger {
		t.Fatal("wrapped failure lost its code")
	}
	if MigrationFailureCode(nil) != "" {
		t.Fatal("nil error must have no code")
	}
	if code := MigrationFailureCode(&os.PathError{Op: "open", Path: "/x", Err: os.ErrPermission}); code != MigrationFailurePermission {
		t.Fatalf("permission code = %q", code)
	}
	if code := MigrationFailureCode(errors.New("attempt to write a read-only database")); code != MigrationFailurePermission {
		t.Fatalf("read-only code = %q", code)
	}
	if code := MigrationFailureCode(errors.New("something else")); code != MigrationFailureUnknown {
		t.Fatalf("unknown code = %q", code)
	}
}

// TestMigrationFailureClassifiesBusyLock 用真实的写锁冲突确认 SQLITE_BUSY 被归类为 busy，
// 而不是落进未知错误。
func TestMigrationFailureClassifiesBusyLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	holder, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	writer, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := holder.ExecContext(t.Context(), "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(t.Context(), "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(t.Context(), "CREATE TABLE cov_busy(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	locked, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locked.Close() })
	if _, err := locked.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.ExecContext(t.Context(), "INSERT INTO cov_busy(id) VALUES(1)")
	if _, err := locked.ExecContext(t.Context(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if writeErr == nil || !isSQLiteLockError(writeErr) {
		t.Fatalf("locked write error = %v", writeErr)
	}
	if code := MigrationFailureCode(writeErr); code != MigrationFailureBusy {
		t.Fatalf("busy code = %q", code)
	}
	if err := waitForSQLiteRetry(newImmediateCanceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("retry wait must honor cancellation: %v", err)
	}
}

func newImmediateCanceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
