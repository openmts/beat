package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func seedRestoreSession(t *testing.T, fixture *backupFixture) model.AdminSession {
	t.Helper()
	admins := store.NewAdminStore(fixture.sqlite.DB)
	now, until := model.NowUTC(), model.NowUTC().Add(time.Hour)
	user := model.AdminUser{ID: "restore-owner", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "test-hash", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: "restore-session", UserID: user.ID, TokenHash: []byte("session-cookie-hash"), TokenPrefix: "session", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return session
}

func stageAuthSnapshot(t *testing.T, fixture *backupFixture) string {
	t.Helper()
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(target, "backups", record.Filename)
	if err := copyFile(filepath.Join(fixture.dataDir, "backups", record.Filename), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(filepath.Join(target, "restore.pending.json"), restoreJournal(record.ID, archive)); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestRestoreNeverReactivatesArchivedAdministratorSessions(t *testing.T) {
	fixture := newBackupFixture(t)
	session := seedRestoreSession(t, fixture)
	seedPendingRestoreLogin(t, fixture)
	target := stageAuthSnapshot(t, fixture)
	databasePath := filepath.Join(target, "beat.db")
	if err := ApplyPending(t.Context(), target, databasePath, filepath.Join(target, "beat_mts")); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	restored, err := store.NewAdminStore(database.DB).GetSessionByTokenHash(t.Context(), session.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if restored != nil && (restored.RevokedAt == nil || restored.ReauthenticatedUntil != nil) {
		t.Fatal("restoring an old snapshot reactivated a browser session and recent proof")
	}
	var pending int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_oidc_transactions").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatal("restoring a snapshot retained pending identity transactions")
	}
}

func seedPendingRestoreLogin(t *testing.T, fixture *backupFixture) {
	t.Helper()
	oidc := store.NewOIDCStore(fixture.sqlite.DB)
	now := model.NowUTC()
	provider := model.OIDCProvider{ID: "provider", Slug: "identity", DisplayName: "Identity", IssuerURL: "https://identity.example.test", ClientID: "beat", Scopes: []string{"openid"}, Enabled: true, ConfigurationVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := oidc.CreateProvider(t.Context(), &provider); err != nil {
		t.Fatal(err)
	}
	secrets, ok := fixture.service.secretLifecycle.(secretbox.ResourceService)
	if !ok {
		t.Fatal("fixture requires shared application secrets")
	}
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("fixture-verifier"), AAD: secretbox.OIDCTransactionAAD("transaction", provider.ID), Persist: func(ciphertext []byte) error {
		return oidc.CreateTransaction(t.Context(), &model.OIDCTransaction{ID: "transaction", StateHash: bytes.Repeat([]byte{1}, 32), BrowserBindingHash: bytes.Repeat([]byte{2}, 32), NonceHash: bytes.Repeat([]byte{3}, 32),
			CodeVerifierEncrypted: ciphertext, ProviderID: provider.ID, ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin, RedirectURI: "https://beat.example.test/callback", ReturnPath: "/admin", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	}})
}

func TestRestoreAuthenticationFailureLeavesExistingDatabaseUnchanged(t *testing.T) {
	fixture := newBackupFixture(t)
	_ = seedRestoreSession(t, fixture)
	if _, err := fixture.sqlite.DB.Exec(`CREATE TRIGGER refuse_restore_revoke BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	target := stageAuthSnapshot(t, fixture)
	databasePath := filepath.Join(target, "beat.db")
	original := []byte("existing data must remain unchanged")
	if err := os.WriteFile(databasePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPending(t.Context(), target, databasePath, filepath.Join(target, "beat_mts")); err == nil {
		t.Fatal("restored active sessions were accepted")
	}
	actual, err := os.ReadFile(databasePath)
	if err != nil || !bytes.Equal(original, actual) {
		t.Fatal("authentication validation failure replaced existing data")
	}
}
