package testutil_test

import (
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

// 该用例放在外部测试包：internal/testutil 本身不能反向依赖 internal/store，
// 否则 store 包内测试文件导入 testutil 时会形成导入环。
func openFixtureDatabase(t *testing.T) *store.SQLiteStore {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "secrets.db"))
	if err != nil {
		t.Fatalf("new SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestNewSecretsWritesThroughFixture(t *testing.T) {
	database := openFixtureDatabase(t)
	secrets := testutil.NewSecrets(t, database.DB)
	var persisted []byte
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("fixture TOTP"),
		AAD: secretbox.TOTPSecretAAD("user-1"), Persist: func(ciphertext []byte) error {
			persisted = ciphertext
			return nil
		}})
	if len(persisted) == 0 {
		t.Fatal("fixture did not persist ciphertext")
	}
	plaintext, err := secrets.OpenResource(t.Context(), persisted, secretbox.TOTPSecretAAD("user-1"))
	if err != nil {
		t.Fatalf("open sealed fixture: %v", err)
	}
	if string(plaintext) != "fixture TOTP" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestOpenSecretsAcceptsExplicitDirectory(t *testing.T) {
	database := openFixtureDatabase(t)
	service := testutil.OpenSecrets(t, database.DB, t.TempDir())
	if service == nil {
		t.Fatal("open secrets fixture returned nil service")
	}
}
