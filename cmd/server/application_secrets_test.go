package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestSecretCLIParsesAndReportsStableLockPath(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "data")
	var output bytes.Buffer
	handled, err := handleSecretCommand([]string{"secrets", "lock-path", "--data-dir", directory}, &output)
	want, pathErr := applicationsecrets.LifecycleLockPath(directory)
	if err != nil || pathErr != nil || !handled || strings.TrimSpace(output.String()) != want {
		t.Fatalf("lock CLI: %v, %v", err, pathErr)
	}
	if handled, err := handleSecretCommand([]string{"-version"}, &output); handled || err != nil {
		t.Fatal("ordinary command intercepted")
	}
	for _, args := range [][]string{{"secrets"}, {"secrets", "bad"}, {"secrets", "lock-path", "--bad"}, {"secrets", "lock-path", "extra"}, {"secrets", "lock-path", "--data-dir", ""}} {
		if handled, err := handleSecretCommand(args, &output); !handled || err == nil {
			t.Fatalf("invalid args accepted: %q", args)
		}
	}
}

func TestSecretCLIRotatesOnlyInPrivateTemporaryState(t *testing.T) {
	directory := t.TempDir()
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.OpenSecrets(t, database.DB, directory)
	secrets.Close()
	var output bytes.Buffer
	handled, err := handleSecretCommand([]string{"secrets", "rotate-root", "--data-dir", directory}, &output)
	if os.Geteuid() != 0 {
		if !handled || err == nil {
			t.Fatal("unprivileged root rotation accepted")
		}
		return
	}
	if !handled || err != nil || !strings.Contains(output.String(), "completed") {
		t.Fatalf("rotation CLI: %v", err)
	}
	if err := applicationsecrets.ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func TestSecretsReadinessNeverExposesUnderlyingError(t *testing.T) {
	operations := newTestOperability()
	operations.secrets = fakeSQLiteMonitor{readyErr: errors.New("secret key material")}
	check := operations.secretsCheck(context.Background())
	if check.Status != "error" || check.Detail != "unavailable" {
		t.Fatal("unsafe readiness detail")
	}
	operations.secrets = nil
	if operations.secretsCheck(t.Context()).Status != "error" {
		t.Fatal("missing secrets accepted")
	}
}

func TestServerLeaseRejectsAFileInTheDataDirectoryParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lease, err := acquireServerLease(filepath.Join(path, "data", "beat.db")); err == nil {
		_ = lease.Close()
		t.Fatal("file parent accepted")
	}
}
