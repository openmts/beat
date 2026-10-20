package applicationsecrets

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func ownerRotationFixture(t *testing.T) (*Service, *store.SQLiteStore, RotationActor) {
	t.Helper()
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	now := service.now()
	recent := now.Add(5 * time.Minute)
	admins := store.NewAdminStore(database.DB)
	if err := admins.CreateUser(t.Context(), &model.AdminUser{ID: "owner", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner,
		PasswordHash: "fixture", Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: "owner-session", UserID: "owner", TokenHash: bytes.Repeat([]byte{3}, 32), TokenPrefix: "prefix",
		CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), ReauthenticatedUntil: &recent}); err != nil {
		t.Fatal(err)
	}
	return service, database, RotationActor{UserID: "owner", SessionID: "owner-session", RequestID: "request-1", IPAddress: "127.0.0.1", UserAgent: "test browser"}
}

func TestOwnerRotationRechecksAuthorityInsideActivationTransaction(t *testing.T) {
	cases := map[string]string{
		"revoked":      "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP",
		"demoted":      "UPDATE admin_users SET role='admin'",
		"disabled":     "UPDATE admin_users SET enabled=0",
		"expired":      "UPDATE admin_sessions SET absolute_expires_at='2000-01-01 00:00:00'",
		"idle_expired": "UPDATE admin_sessions SET idle_expires_at='2000-01-01 00:00:00'",
		"stale_proof":  "UPDATE admin_sessions SET reauthenticated_until='2000-01-01 00:00:00'",
		"no_proof":     "UPDATE admin_sessions SET reauthenticated_until=NULL",
	}
	for name, statement := range cases {
		t.Run(name, func(t *testing.T) {
			service, database, actor := ownerRotationFixture(t)
			// HTTP 前置身份检查之后发生的撤销/降权，必须由同一激活事务再次拒绝。
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := service.StartOwnerRotation(t.Context(), actor); err == nil {
				t.Fatal("stale authority activated a key")
			}
			state, err := readState(t.Context(), database.DB)
			if err != nil || state.Generation != 1 || state.Phase != "ready" {
				t.Fatalf("denied rotation changed state: %v", err)
			}
		})
	}
}

func TestOwnerRotationAuditAndKeyActivationCommitTogether(t *testing.T) {
	for _, action := range []string{"ABORT, 'audit failed'", "IGNORE"} {
		t.Run(action, func(t *testing.T) {
			service, database, actor := ownerRotationFixture(t)
			if _, err := database.DB.Exec("CREATE TRIGGER reject_rotation_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(" + action + "); END"); err != nil {
				t.Fatal(err)
			}
			if _, err := service.StartOwnerRotation(t.Context(), actor); err == nil {
				t.Fatal("audit failure activated a key")
			}
			state, err := readState(t.Context(), database.DB)
			if err != nil || state.Generation != 1 || state.RotationID != "" {
				t.Fatal("audit failure changed rotation state")
			}
			keys, err := readRegistry(t.Context(), database.DB)
			if err != nil || len(keys) != 1 || keys[0].State != "active" {
				t.Fatal("audit failure changed key registry")
			}
			if _, err := database.DB.Exec("DROP TRIGGER reject_rotation_audit"); err != nil {
				t.Fatal(err)
			}
			status, err := service.StartOwnerRotation(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			service.wg.Wait()
			var id, request, actorID, detail string
			if err := database.DB.QueryRow("SELECT resource_id,request_id,actor_id,detail_json FROM admin_audit_events WHERE action='application_secret.rotation.start'").Scan(&id, &request, &actorID, &detail); err != nil {
				t.Fatal(err)
			}
			if id == "" || id != status.RotationID || request != actor.RequestID || actorID != actor.UserID || strings.Contains(detail, "wrapped") || strings.Contains(detail, "key_id") {
				t.Fatal("rotation audit lost attribution or exposed ciphertext")
			}
		})
	}
}

func TestOwnerRotationRejectsMissingOrMismatchedActor(t *testing.T) {
	service, _, actor := ownerRotationFixture(t)
	for _, invalid := range []RotationActor{{}, {UserID: actor.UserID}, {UserID: "other", SessionID: actor.SessionID}} {
		if _, err := service.StartOwnerRotation(t.Context(), invalid); err == nil {
			t.Fatal("unbound actor accepted")
		}
	}
}

func TestOwnerProofExpirationBeforeCommitRollsBackActivation(t *testing.T) {
	service, database, actor := ownerRotationFixture(t)
	now := service.now()
	service.now = func() time.Time { return now }
	service.fault = func(phase string) error {
		if phase == "rotation_before_activate" {
			now = now.Add(6 * time.Minute)
		}
		return nil
	}
	if _, err := service.StartOwnerRotation(t.Context(), actor); err != ErrRotationRecentAuth {
		t.Fatalf("expired proof accepted: %v", err)
	}
	state, err := readState(t.Context(), database.DB)
	if err != nil || state.Generation != 1 {
		t.Fatal("expired proof changed generation")
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events").Scan(&count); err != nil || count != 0 {
		t.Fatal("expired proof wrote success audit")
	}
}
