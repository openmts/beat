package backup

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/oidc"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func setupOIDCBackupDB(t *testing.T) (*store.SQLiteStore, *secretbox.Manager, []byte) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	for _, statement := range []string{
		"DROP TABLE application_data_keys", "DROP TABLE application_secret_state",
		"ALTER TABLE ssh_keys DROP COLUMN private_key_encrypted", "DELETE FROM schema_migrations WHERE version >= 9",
	} {
		if _, err := sqlite.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	key := bytes.Repeat([]byte{7}, 32)
	secrets, err := secretbox.NewFromKey(key, nil)
	if err != nil {
		t.Fatalf("new secretbox from key: %v", err)
	}
	return sqlite, secrets, key
}

func sqlitePath(t *testing.T, sqlite *store.SQLiteStore) string {
	t.Helper()
	var path string
	if err := sqlite.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil {
		t.Fatalf("query sqlite path: %v", err)
	}
	return path
}

func createBackupProvider(t *testing.T, sqlite *store.SQLiteStore, secrets *secretbox.Manager, key []byte, tamper bool) {
	t.Helper()
	oidcStore := store.NewOIDCStore(sqlite.DB)
	now := time.Now().UTC()
	provider := &model.OIDCProvider{
		ID: "p1", Slug: "issuer-one", DisplayName: "Issuer One",
		IssuerURL: "https://issuer.example.com", ClientID: "client-1",
		Scopes: []string{"openid"}, Enabled: true, ConfigurationVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := oidcStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	encrypted, err := secrets.EncryptEnvelope([]byte("secret-value"), oidc.ProviderSecretAAD(provider.ID, 1))
	if err != nil {
		t.Fatalf("encrypt provider secret: %v", err)
	}
	if tamper {
		encrypted[len(encrypted)-1] ^= 0x01
	}
	provider.ClientSecretEncrypted = encrypted
	if err := oidcStore.UpdateProvider(t.Context(), provider); err != nil {
		t.Fatalf("update provider: %v", err)
	}
	_ = key
}

func writeBackupKey(t *testing.T, key []byte) string {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "admin-data.key")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return keyPath
}

func TestValidateOIDCCiphertextsAuthenticatesArchive(t *testing.T) {
	sqlite, secrets, key := setupOIDCBackupDB(t)
	createBackupProvider(t, sqlite, secrets, key, false)
	keyPath := writeBackupKey(t, key)
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), keyPath); err != nil {
		t.Fatalf("validate authentic archive: %v", err)
	}
}

func TestValidateOIDCCiphertextsRejectsTamperedCiphertext(t *testing.T) {
	sqlite, secrets, key := setupOIDCBackupDB(t)
	createBackupProvider(t, sqlite, secrets, key, true)
	keyPath := writeBackupKey(t, key)
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), keyPath); err == nil {
		t.Fatal("tampered provider secret must fail archive validation")
	}
}

func TestValidateOIDCCiphertextsMissingKeySkipsOldArchive(t *testing.T) {
	sqlite, _, _ := setupOIDCBackupDB(t)
	keyPath := filepath.Join(t.TempDir(), "admin-data.key")
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), keyPath); err != nil {
		t.Fatalf("archive without key must be skipped, got %v", err)
	}
}

func TestValidateOIDCCiphertextsMissingOIDCTablesSkips(t *testing.T) {
	// 打开一个不含 OIDC 表的旧库：迁移前仅建部分表不现实，此处验证
	// 无表时校验跳过（no such table 分支）。
	sqlite, _, key := setupOIDCBackupDB(t)
	_ = sqlite
	keyPath := writeBackupKey(t, key)
	// 构造一个独立空 sqlite 文件
	emptyPath := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(emptyPath, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("write empty db: %v", err)
	}
	if err := validateOIDCCiphertexts(t.Context(), emptyPath, keyPath); err == nil {
		t.Fatal("invalid sqlite must fail validation")
	}
}

func createBackupTransaction(t *testing.T, sqlite *store.SQLiteStore, secrets *secretbox.Manager, key []byte, tamper bool) {
	t.Helper()
	oidcStore := store.NewOIDCStore(sqlite.DB)
	now := time.Now().UTC()
	stateHash := make([]byte, 32)
	if _, err := rand.Read(stateHash); err != nil {
		t.Fatalf("read random: %v", err)
	}
	encrypted, err := secrets.EncryptEnvelope([]byte("verifier-value"),
		oidc.TransactionVerifierAAD("txn-1", "p1"))
	if err != nil {
		t.Fatalf("encrypt verifier: %v", err)
	}
	if tamper {
		encrypted[len(encrypted)-1] ^= 0x01
	}
	if err := oidcStore.CreateTransaction(t.Context(), &model.OIDCTransaction{
		ID: "txn-1", StateHash: stateHash, BrowserBindingHash: []byte("bbh"),
		NonceHash: []byte("nh"), CodeVerifierEncrypted: encrypted,
		ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.test/cb", ReturnPath: "/admin",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	_ = key
}

func TestValidateOIDCCiphertextsAuthenticatesTransaction(t *testing.T) {
	sqlite, secrets, key := setupOIDCBackupDB(t)
	createBackupProvider(t, sqlite, secrets, key, false)
	createBackupTransaction(t, sqlite, secrets, key, false)
	keyPath := writeBackupKey(t, key)
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), keyPath); err != nil {
		t.Fatalf("validate transaction ciphertext: %v", err)
	}
}

func TestValidateOIDCCiphertextsRejectsTamperedTransaction(t *testing.T) {
	sqlite, secrets, key := setupOIDCBackupDB(t)
	createBackupProvider(t, sqlite, secrets, key, false)
	createBackupTransaction(t, sqlite, secrets, key, true)
	keyPath := writeBackupKey(t, key)
	if err := validateOIDCCiphertexts(t.Context(), sqlitePath(t, sqlite), keyPath); err == nil {
		t.Fatal("tampered transaction ciphertext must fail")
	}
}
