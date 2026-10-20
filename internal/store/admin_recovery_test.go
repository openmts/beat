package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/testutil"
)

func recoveryStoreFixture(t *testing.T) (*SQLiteStore, *AdminStore, AdminRecoveryWrite) {
	t.Helper()
	database := setupTestDB(t)
	secrets := testutil.NewSecrets(t, database.DB)
	admins := NewAdminStore(database.DB)
	now, until := model.NowUTC(), model.NowUTC().Add(time.Hour)
	owner := model.AdminUser{ID: "owner-id", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: false, PasswordHash: "previous", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now, TOTPEnabledAt: &now}
	testutil.WriteSecret(t, secrets, testutil.SecretWrite{Plaintext: []byte("fixture TOTP"), AAD: secretbox.TOTPSecretAAD(owner.ID), Persist: func(ciphertext []byte) error {
		owner.TOTPSecretEncrypted = ciphertext
		return admins.CreateUser(t.Context(), &owner)
	}})
	other := model.AdminUser{ID: "admin-id", Username: "admin", DisplayName: "Administrator", Role: model.AdminRoleAdmin, Enabled: true, PasswordHash: "other-password", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &other); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{owner.ID, other.ID} {
		if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: id + "-session", UserID: id, TokenHash: []byte(id), TokenPrefix: id, CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
			t.Fatal(err)
		}
	}
	write := AdminRecoveryWrite{UserID: owner.ID, Username: owner.Username, PasswordHash: "replacement-derived-hash", At: now}
	if err := database.DB.QueryRow("SELECT generation,root_generation FROM application_secret_state WHERE id=1").Scan(&write.SecretGeneration, &write.RootGeneration); err != nil {
		t.Fatal(err)
	}
	return database, admins, write
}

func TestRecoveryAtomicallyUnlocksSelectedOwnerAndRevokesAllSessions(t *testing.T) {
	database, admins, write := recoveryStoreFixture(t)
	if err := admins.RecoverOwner(t.Context(), write); err != nil {
		t.Fatal(err)
	}
	owner, err := admins.GetUserByID(t.Context(), write.UserID)
	if err != nil || !owner.Enabled || owner.TOTPEnabledAt != nil || len(owner.TOTPSecretEncrypted) != 0 || owner.PasswordHash != write.PasswordHash {
		t.Fatal("owner recovery incomplete")
	}
	other, err := admins.GetUserByID(t.Context(), "admin-id")
	if err != nil || other.PasswordHash != "other-password" || other.Role != model.AdminRoleAdmin {
		t.Fatal("unrelated account was changed")
	}
	var active int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_sessions WHERE revoked_at IS NULL OR reauthenticated_until IS NOT NULL").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatal("old sessions or recent proofs survived")
	}
	var detail string
	if err := database.DB.QueryRow("SELECT detail_json FROM admin_audit_events WHERE action='security.break_glass_recovery'").Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "owner") || strings.Contains(detail, write.PasswordHash) {
		t.Fatal("recovery audit lost the target snapshot or exposed a credential")
	}
	var audit struct {
		Username string   `json:"target_username"`
		Actions  []string `json:"actions"`
	}
	if err := json.Unmarshal([]byte(detail), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Username != write.Username || len(audit.Actions) != 5 {
		t.Fatal("recovery audit does not identify the exact target and completed actions")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", write, write), write.PasswordHash) {
		t.Fatal("recovery struct formatter exposes password hash")
	}
}

func TestRecoveryRejectsStaleGenerationAndNonOwnerTargets(t *testing.T) {
	for _, kind := range []string{"generation", "root generation", "role", "missing user", "missing input"} {
		t.Run(kind, func(t *testing.T) {
			_, admins, write := recoveryStoreFixture(t)
			switch kind {
			case "generation":
				write.SecretGeneration++
			case "root generation":
				write.RootGeneration++
			case "role":
				write.UserID, write.Username = "admin-id", "admin"
			case "missing user":
				write.UserID = "missing"
			case "missing input":
				write.PasswordHash = ""
			}
			if err := admins.RecoverOwner(t.Context(), write); err == nil {
				t.Fatal("invalid recovery accepted")
			}
			owner, err := admins.GetUserByID(t.Context(), "owner-id")
			if err != nil || owner.Enabled || owner.PasswordHash != "previous" || owner.TOTPEnabledAt == nil {
				t.Fatal("rejected recovery changed owner")
			}
		})
	}
}

func TestRecoveryEveryWriteFailureRollsBackAllAccountChanges(t *testing.T) {
	for _, table := range []string{"admin_users", "admin_sessions", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			database, admins, write := recoveryStoreFixture(t)
			event := "UPDATE"
			if table == "admin_audit_events" {
				event = "INSERT"
			}
			statement := "CREATE TRIGGER fail_recovery BEFORE " + event + " ON " + table + " BEGIN SELECT RAISE(ABORT,'recovery fault'); END"
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err := admins.RecoverOwner(t.Context(), write); err == nil {
				t.Fatal("write failure accepted")
			}
			owner, err := admins.GetUserByID(t.Context(), write.UserID)
			if err != nil || owner.Enabled || owner.PasswordHash != "previous" || owner.TOTPEnabledAt == nil {
				t.Fatal("failed recovery partially changed owner")
			}
			session, err := admins.GetSessionByID(t.Context(), "admin-id-session")
			if err != nil || session.RevokedAt != nil {
				t.Fatal("failed recovery partially revoked sessions")
			}
		})
	}
}
