package operatorrecovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func fixture(t *testing.T) Config {
	t.Helper()
	config := Config{DataDir: privateTemp(t)}
	config.DatabasePath = filepath.Join(config.DataDir, "beat.db")
	database, err := store.NewSQLiteStore(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := applicationsecrets.Open(t.Context(), applicationsecrets.Config{Database: database.DB, DataDir: config.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	owner := model.AdminUser{ID: "owner-id", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, PasswordHash: "old-secret-hash", CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now, TOTPEnabledAt: &now}
	err = secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		ciphertext, sealErr := access.Seal([]byte("totp-secret"), secretbox.TOTPSecretAAD(owner.ID))
		if sealErr != nil {
			return sealErr
		}
		owner.TOTPSecretEncrypted = ciphertext
		return store.NewAdminStore(database.DB).CreateUser(t.Context(), &owner)
	})
	if err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Hour)
	if err := store.NewAdminStore(database.DB).CreateSession(t.Context(), &model.AdminSession{ID: "session", UserID: owner.ID, TokenHash: []byte("secret-token"), TokenPrefix: "secret", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until}); err != nil {
		t.Fatal(err)
	}
	secrets.Close()
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestUnlockAndInspect(t *testing.T) {
	config := fixture(t)
	before, err := os.ReadFile(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	after, err := os.ReadFile(config.DatabasePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("inspect changed database")
	}
	status := session.Status
	if !status.Integrity || !status.Keys || !status.LocalLogin || len(status.Owners) != 1 || status.ActiveSessions != 1 || !status.Owners[0].TOTP {
		t.Fatalf("status: %+v", status)
	}
	content, err := json.Marshal(status)
	if err != nil || strings.Contains(string(content), "secret") || strings.Contains(string(content), "owner-id") {
		t.Fatal("sensitive inspection output")
	}
	request := UnlockInput{Username: " OWNER ", Password: "replacement password", Repeat: "replacement password", Confirmation: session.Confirmation("owner")}
	if err := session.Unlock(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	database, err := openDatabase(config.DatabasePath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	user, err := store.NewAdminStore(database).GetUserByUsername(t.Context(), "owner")
	if err != nil || !user.Enabled || user.TOTPEnabled() {
		t.Fatal("incomplete owner recovery")
	}
	valid, err := adminauth.NewPasswordHasher(adminauth.DefaultPasswordParams(), nil).Verify(request.Password, user.PasswordHash)
	if err != nil || !valid {
		t.Fatal("replacement password rejected")
	}
	var active, audits int
	if err := database.QueryRow("SELECT COUNT(*) FROM admin_sessions WHERE revoked_at IS NULL").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='security.break_glass_recovery'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if active != 0 || audits != 1 {
		t.Fatal("missing atomic revocation and audit")
	}
}

func TestRejectedUnlockLeavesDatabaseUnchanged(t *testing.T) {
	config := fixture(t)
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	before, err := os.ReadFile(config.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	valid := UnlockInput{Username: "owner", Password: "replacement password", Repeat: "replacement password", Confirmation: session.Confirmation("owner")}
	for _, change := range []func(*UnlockInput){
		func(input *UnlockInput) { input.Username = "missing" },
		func(input *UnlockInput) { input.Confirmation = "wrong" },
		func(input *UnlockInput) { input.Repeat = "different" },
		func(input *UnlockInput) { input.Password = "short"; input.Repeat = "short" },
	} {
		input := valid
		change(&input)
		if err := session.Unlock(t.Context(), input); err == nil {
			t.Fatal("invalid unlock accepted")
		}
	}
	session.Status.PendingRestore = true
	if err := session.Unlock(t.Context(), valid); err == nil {
		t.Fatal("pending accepted")
	}
	if err := session.StageRestore(t.Context(), "missing"); err == nil {
		t.Fatal("pending stage accepted")
	}
	after, err := os.ReadFile(config.DatabasePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid input mutated original")
	}
}

func TestPreflightRefusals(t *testing.T) {
	for _, mutation := range []string{
		"INSERT INTO schema_migrations VALUES(9999,'future',CURRENT_TIMESTAMP)",
		"DELETE FROM schema_migrations WHERE version=2",
		"UPDATE application_secret_state SET phase='rotating'",
		"UPDATE admin_users SET totp_secret_encrypted=x'001122'",
	} {
		t.Run(mutation, func(t *testing.T) {
			config := fixture(t)
			database, err := openDatabase(config.DatabasePath, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			if session, err := Open(t.Context(), config); err == nil {
				_ = session.Close()
				t.Fatal("invalid state accepted")
			}
		})
	}
	config := fixture(t)
	lease, err := applicationsecrets.AcquireLease(config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), config); !errors.Is(err, applicationsecrets.ErrLifecycleBusy) {
		t.Fatal("lock ignored")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(config.DataDir, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), config); err == nil {
		t.Fatal("missing key accepted")
	}
}
