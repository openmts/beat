package applicationsecrets

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestBackfillRollsBackAndResumesAtEveryPhase(t *testing.T) {
	for _, phase := range []string{"bootstrap_before_commit", "batch_before_commit", "migration_before_complete", "sanitized"} {
		t.Run(phase, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			root := bytes.Repeat([]byte{7}, rootKeyBytes)
			seedLegacyTOTP(t, database, root)
			_ = seedLegacyConsumers(t, database, root)
			if err := publishNewPrivateFile(filepath.Join(directory, "admin-data.key"), root); err != nil {
				t.Fatal(err)
			}
			interrupted := errors.New("interrupted")
			_, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, fault: func(current string) error {
				if current == phase {
					return interrupted
				}
				return nil
			}})
			if !errors.Is(err, interrupted) {
				t.Fatalf("phase %s returned %v", phase, err)
			}
			if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err == nil {
				t.Fatal("incomplete backfill was ready for restore")
			}
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
		})
	}
}

func TestBootstrapAndBackfillRejectDatabaseFailures(t *testing.T) {
	cases := map[string]string{
		"missing_state":    "DROP TABLE application_secret_state",
		"missing_registry": "DROP TABLE application_data_keys",
		"insert_key":       "CREATE TRIGGER deny_key BEFORE INSERT ON application_data_keys BEGIN SELECT RAISE(ABORT,'injected'); END",
		"ignore_key":       "CREATE TRIGGER ignore_key BEFORE INSERT ON application_data_keys BEGIN SELECT RAISE(IGNORE); END",
		"state_write":      "CREATE TRIGGER deny_state BEFORE UPDATE ON application_secret_state BEGIN SELECT RAISE(ABORT,'injected'); END",
		"cipher_write":     "CREATE TRIGGER deny_cipher BEFORE UPDATE ON admin_users BEGIN SELECT RAISE(ABORT,'injected'); END",
		"missing_source":   "DROP TABLE ssh_keys",
		"corrupt_totp":     "UPDATE admin_users SET totp_secret_encrypted=X'00'",
		"missing_totp":     "UPDATE admin_users SET totp_secret_encrypted=NULL",
	}
	for name, statement := range cases {
		t.Run(name, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			root := bytes.Repeat([]byte{7}, rootKeyBytes)
			seedLegacyTOTP(t, database, root)
			if err := publishNewPrivateFile(filepath.Join(directory, "admin-data.key"), root); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
				service.Close()
				t.Fatal("unsafe migration served")
			}
		})
	}
}

func TestSecretInitializationFailsClosedForEntropyAndMissingSources(t *testing.T) {
	for _, available := range []int{0, rootKeyBytes, rootKeyBytes + 48} {
		t.Run(string(rune('a'+available)), func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			if service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, Random: bytes.NewReader(bytes.Repeat([]byte{3}, available))}); err == nil {
				service.Close()
				t.Fatal("insufficient entropy accepted")
			}
		})
	}
	database, directory := lifecycleTestDB(t)
	if _, err := database.DB.Exec("DROP TABLE admin_users"); err != nil {
		t.Fatal(err)
	}
	if service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
		service.Close()
		t.Fatal("missing resource inventory accepted")
	}
}

func TestResourceDecodingNeverFallsBackToPlaintext(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	cases := []rewriteInput{
		{source: resourceSource{name: "ssh"}, resource: secretResource{ciphertext: []byte("corrupt"), legacyPlaintext: []byte("private")}, legacy: true},
		{source: resourceSource{name: "ssh"}, legacy: true},
		{source: resourceSource{name: "unknown"}, legacy: true},
		{source: resourceSource{name: "ssh"}, resource: secretResource{legacyPlaintext: []byte("private")}},
	}
	for _, input := range cases {
		if _, err := service.resourcePlaintext(input); err == nil {
			t.Fatal("unsafe legacy fallback accepted")
		}
	}
	sealed, err := service.ring.Seal([]byte("key"), secretbox.SSHPrivateKeyAAD("id"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.resourcePlaintext(rewriteInput{resource: secretResource{ciphertext: sealed, legacyPlaintext: []byte("ambiguous")}}); err == nil {
		t.Fatal("ambiguous SSH source accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.Ready(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := service.WithWrite(ctx, func(secretbox.ResourceAccess) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := service.OpenResource(ctx, sealed, secretbox.SSHPrivateKeyAAD("id")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := service.AcquireBackup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := service.StartRotation(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	service.unhealthy.Store(true)
	if err := service.WithWrite(t.Context(), func(secretbox.ResourceAccess) error { return nil }); !errors.Is(err, ErrUnhealthy) {
		t.Fatal(err)
	}
	if _, err := service.OpenResource(t.Context(), sealed, secretbox.SSHPrivateKeyAAD("id")); !errors.Is(err, ErrUnhealthy) {
		t.Fatal(err)
	}
}
