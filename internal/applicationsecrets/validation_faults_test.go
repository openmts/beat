package applicationsecrets

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestSnapshotRejectsEveryRegistryMismatch(t *testing.T) {
	cases := map[string]string{
		"missing_key":             "DELETE FROM application_data_keys",
		"unknown_active":          "UPDATE application_secret_state SET active_key_id='unknown'",
		"wrong_root":              "UPDATE application_secret_state SET root_fingerprint='wrong'",
		"wrong_generation":        "UPDATE application_secret_state SET generation=generation+1",
		"invalid_root_generation": "UPDATE application_secret_state SET root_generation=0",
		"cursor":                  "UPDATE application_secret_state SET cursor_id='unfinished'",
		"pending":                 "UPDATE application_secret_state SET phase='rotating'",
		"no_active":               "UPDATE application_data_keys SET state='decrypt_only'",
		"corrupt_wrap":            "UPDATE application_data_keys SET wrapped_key=zeroblob(90)",
		"bad_creation":            "UPDATE application_data_keys SET created_at=0",
		"missing_state":           "DELETE FROM application_secret_state",
		"unknown_cipher":          "INSERT INTO ssh_keys(id,name,key_type,public_key,private_key_encrypted,created_at) VALUES('bad','bad','ed25519','public',X'01',CURRENT_TIMESTAMP)",
	}
	for name, statement := range cases {
		t.Run(name, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
}

func TestResourceValidatorRequiresBoundedAuthenticatedValues(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	ciphertext, err := service.ring.Seal([]byte("private"), secretbox.SSHPrivateKeyAAD("id"))
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []secretResource{{required: true}, {legacyPlaintext: []byte("private")}, {ciphertext: []byte("invalid")},
		{ciphertext: ciphertext, aad: secretbox.SSHPrivateKeyAAD("different")}} {
		if _, err := validateResource(resource, validationConfig{ring: service.ring}); err == nil {
			t.Fatal("unauthenticated resource accepted")
		}
	}
	unknown := append([]byte(nil), ciphertext...)
	unknown[10] ^= 1
	if _, err := service.ring.Open(unknown, secretbox.SSHPrivateKeyAAD("id")); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, err := validateResource(secretResource{ciphertext: unknown}, validationConfig{ring: service.ring, requireActive: true}); err == nil {
		t.Fatal("old key reference accepted as complete")
	}
	if _, err := database.DB.Exec("INSERT INTO ssh_keys(id,name,key_type,public_key,private_key_encrypted,created_at) VALUES('big','big','ed25519','public',zeroblob(?),CURRENT_TIMESTAMP)", secretbox.MaximumSecretBytes+129); err != nil {
		t.Fatal(err)
	}
	if _, err := resourceSources(false)[3].readBatch(t.Context(), database.DB, ""); err == nil {
		t.Fatal("oversized ciphertext accepted")
	}
}

func TestClosedDatabaseRejectsLifecycleOperations(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"bootstrap":  func() error { return service.bootstrap(t.Context()) },
		"batch":      func() error { return service.migrateBatch(t.Context()) },
		"sanitize":   func() error { return service.sanitize(t.Context()) },
		"reload":     func() error { return service.reload(t.Context()) },
		"rotate":     func() error { return service.activateRotation(t.Context()) },
		"checkpoint": func() error { return checkpointSQLite(t.Context(), database.DB) },
		"registry": func() error {
			_, err := loadKeyring(t.Context(), database.DB, keyringConfig{root: service.root, state: service.state})
			return err
		},
		"resources": func() error {
			_, err := validateResources(t.Context(), database.DB, validationConfig{ring: service.ring})
			return err
		},
	} {
		if err := operation(); err == nil {
			t.Fatalf("%s ignored closed database", name)
		}
	}
}

func TestRotationActivationFailurePreservesCurrentKey(t *testing.T) {
	for name, statement := range map[string]string{
		"demotion":   "CREATE TRIGGER deny_demotion BEFORE UPDATE ON application_data_keys BEGIN SELECT RAISE(ABORT,'denied'); END",
		"generation": "UPDATE application_secret_state SET generation=9223372036854775807",
		"state":      "CREATE TRIGGER deny_state BEFORE UPDATE ON application_secret_state BEGIN SELECT RAISE(ABORT,'denied'); END",
	} {
		t.Run(name, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.Close)
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := service.StartRotation(t.Context()); err == nil {
				t.Fatal("failed activation accepted")
			}
		})
	}
}

type invalidAffectedResult struct{}

func (invalidAffectedResult) LastInsertId() (int64, error) { return 0, nil }

func (invalidAffectedResult) RowsAffected() (int64, error) {
	return 0, errors.New("unsupported affected rows")
}

func TestAffectedRowErrorsAreNotDiscarded(t *testing.T) {
	if err := requireOne(invalidAffectedResult{}, nil); err == nil {
		t.Fatal("affected row error ignored")
	}
}
