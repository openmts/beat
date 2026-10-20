package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type administratorFixture struct {
	store     *AdminStore
	principal model.AdminPrincipal
	now       time.Time
}

func newAdministratorFixture(t *testing.T) administratorFixture {
	t.Helper()
	target := newAdminTestStore(t)
	now := model.NowUTC()
	user := testAdminUser("owner", model.AdminRoleOwner)
	if err := target.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := administratorSession(user.ID, "current", now)
	if err := target.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return administratorFixture{store: target, principal: model.AdminPrincipal{User: user, Session: session}, now: now}
}

func administratorSession(userID, id string, now time.Time) model.AdminSession {
	until := now.Add(time.Minute)
	return model.AdminSession{ID: id, UserID: userID, TokenHash: []byte(id), TokenPrefix: id, CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), ReauthenticatedUntil: &until}
}

func (f administratorFixture) change(action string, user model.AdminUser) AdministratorChange {
	return AdministratorChange{Principal: f.principal, User: user, Audit: model.AdminAuditEvent{ID: action + user.ID, RequestID: "request", ActorID: f.principal.User.ID, ResourceType: "administrator", ResourceID: user.ID, Action: action, Outcome: model.AuditOutcomeSuccess, CreatedAt: f.now}}
}

func administratorCount(t *testing.T, f administratorFixture, query string) int {
	t.Helper()
	var count int
	if err := f.store.db.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func administratorTrigger(t *testing.T, f administratorFixture, sql string) {
	t.Helper()
	if _, err := f.store.db.ExecContext(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
}

func TestAdministratorMutationCreateAtomicAudit(t *testing.T) {
	f := newAdministratorFixture(t)
	user := testAdminUser("operator", model.AdminRoleAdmin)
	change := f.change(AdministratorCreate, user)
	created, err := f.store.ChangeAdministrator(t.Context(), change)
	if err != nil || created == nil || created.ID != user.ID {
		t.Fatalf("create: %+v %v", created, err)
	}
	var detail string
	if err := f.store.db.QueryRowContext(t.Context(), "SELECT detail_json FROM admin_audit_events WHERE id=?", change.Audit.ID).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "operator") || strings.Contains(detail, user.PasswordHash) {
		t.Fatal(detail)
	}
	user.ID = "duplicate"
	_, err = f.store.ChangeAdministrator(t.Context(), f.change(AdministratorCreate, user))
	if !errors.Is(err, ErrAdminUsernameConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 1 {
		t.Fatal("duplicate success audit")
	}
	administratorTrigger(t, f, "CREATE TRIGGER fail_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit fault'); END")
	user = testAdminUser("rollback", model.AdminRoleAdmin)
	_, err = f.store.ChangeAdministrator(t.Context(), f.change(AdministratorCreate, user))
	if !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	loaded, err := f.store.GetUserByID(t.Context(), user.ID)
	if err != nil || loaded != nil {
		t.Fatalf("uncommitted account: %+v %v", loaded, err)
	}
	if fmt.Sprintf("%#v", change) != "AdministratorChange{redacted}" {
		t.Fatal("change logging is not redacted")
	}
}

func TestAdministratorMutationUpdateAndDeleteRollback(t *testing.T) {
	for _, action := range []string{AdministratorUpdate, AdministratorDelete} {
		t.Run(action, func(t *testing.T) {
			f := newAdministratorFixture(t)
			user := testAdminUser("operator", model.AdminRoleAdmin)
			if err := f.store.CreateUser(t.Context(), &user); err != nil {
				t.Fatal(err)
			}
			session := administratorSession(user.ID, "target", f.now)
			if err := f.store.CreateSession(t.Context(), &session); err != nil {
				t.Fatal(err)
			}
			next := user
			next.Role = model.AdminRoleOwner
			next.DisplayName = "Changed"
			administratorTrigger(t, f, "CREATE TRIGGER fail_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END")
			if _, err := f.store.ChangeAdministrator(t.Context(), f.change(action, next)); !errors.Is(err, ErrAdminMutationUnavailable) {
				t.Fatal(err)
			}
			loaded, err := f.store.GetUserByID(t.Context(), user.ID)
			if err != nil || loaded == nil || loaded.Role != user.Role {
				t.Fatalf("account rollback: %+v %v", loaded, err)
			}
			active, err := f.store.GetSessionByID(t.Context(), session.ID)
			if err != nil || active == nil || active.RevokedAt != nil {
				t.Fatalf("session rollback: %+v %v", active, err)
			}
			administratorTrigger(t, f, "DROP TRIGGER fail_audit")
			if _, err := f.store.ChangeAdministrator(t.Context(), f.change(action, next)); err != nil {
				t.Fatal(err)
			}
			active, err = f.store.GetSessionByID(t.Context(), session.ID)
			if err != nil || (active != nil && active.RevokedAt == nil) {
				t.Fatalf("session not invalidated: %+v %v", active, err)
			}
			if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 1 {
				t.Fatal("expected exactly one success audit")
			}
		})
	}
}

func TestAdministratorMutationPasswordSnapshot(t *testing.T) {
	f := newAdministratorFixture(t)
	second := administratorSession(f.principal.User.ID, "second", f.now)
	if err := f.store.CreateSession(t.Context(), &second); err != nil {
		t.Fatal(err)
	}
	change := f.change(AdministratorPassword, f.principal.User)
	change.ExpectedPasswordHash = change.User.PasswordHash
	change.User.PasswordHash = "replacement-hash"
	updated, err := f.store.ChangeAdministrator(t.Context(), change)
	if err != nil || updated.PasswordHash != "replacement-hash" {
		t.Fatalf("password: %+v %v", updated, err)
	}
	if administratorCount(t, f, "SELECT count(*) FROM admin_sessions WHERE revoked_at IS NULL") != 1 {
		t.Fatal("other sessions survived")
	}
	current, err := f.store.GetSessionByID(t.Context(), "current")
	if err != nil || current.RevokedAt != nil {
		t.Fatal("current session revoked")
	}
	change.Audit.ID = "racing-password"
	change.User.PasswordHash = "stale-replacement"
	if _, err = f.store.ChangeAdministrator(t.Context(), change); !errors.Is(err, ErrAdminMutationConflict) {
		t.Fatalf("stale snapshot: %v", err)
	}
	if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 1 {
		t.Fatal("stale password produced success audit")
	}
}

func TestAdministratorMutationRevocationFailureRollsBack(t *testing.T) {
	f := newAdministratorFixture(t)
	user := testAdminUser("target", model.AdminRoleAdmin)
	if err := f.store.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := administratorSession(user.ID, "target-session", f.now)
	if err := f.store.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	administratorTrigger(t, f, "CREATE TRIGGER ignore_revocation BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(IGNORE); END")
	user.Enabled = false
	if _, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorUpdate, user)); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	loaded, err := f.store.GetUserByID(t.Context(), user.ID)
	if err != nil || !loaded.Enabled {
		t.Fatalf("disable escaped rollback: %+v %v", loaded, err)
	}
	if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 0 {
		t.Fatal("failed revoke produced success audit")
	}
}
