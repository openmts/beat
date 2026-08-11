package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAdminStoreUserLifecycleAndLastOwner(t *testing.T) {
	store := newAdminTestStore(t)
	owner := testAdminUser("owner", model.AdminRoleOwner)
	if err := store.CreateUser(t.Context(), &owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := store.CreateUser(t.Context(), &owner); err == nil {
		t.Fatal("duplicate username succeeded")
	}
	loaded, err := store.GetUserByUsername(t.Context(), "OWNER")
	if err != nil || loaded == nil || loaded.PasswordHash == "" {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
	if err := store.UpdateUser(t.Context(), owner.ID, "Owner", model.AdminRoleAdmin, true); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demote last owner error = %v", err)
	}
	if err := store.DeleteUser(t.Context(), owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("delete last owner error = %v", err)
	}
	if count, err := store.CountUsers(t.Context()); err != nil || count != 1 {
		t.Fatalf("administrator count = %d, %v", count, err)
	}
	users, err := store.ListUsers(t.Context())
	if err != nil || len(users) != 1 {
		t.Fatalf("administrators = %#v, %v", users, err)
	}
	if loaded, err := store.GetUserByID(t.Context(), owner.ID); err != nil || loaded == nil {
		t.Fatalf("administrator by ID = %#v, %v", loaded, err)
	}
	second := testAdminUser("second", model.AdminRoleOwner)
	if err := store.CreateUser(t.Context(), &second); err != nil {
		t.Fatalf("create second owner: %v", err)
	}
	if err := store.UpdateUser(t.Context(), owner.ID, "Former Owner", model.AdminRoleAdmin, true); err != nil {
		t.Fatalf("demote owner with replacement: %v", err)
	}
	if err := store.UpdateUser(t.Context(), owner.ID, "Owner", model.AdminRoleOwner, true); err != nil {
		t.Fatalf("restore owner role: %v", err)
	}
	changedAt := model.NowUTC().Add(time.Minute)
	if err := store.UpdatePassword(t.Context(), owner.ID, "new-hash", changedAt); err != nil {
		t.Fatalf("update password: %v", err)
	}
	if err := store.UpdateTOTP(t.Context(), owner.ID, []byte("new-ciphertext"), &changedAt); err != nil {
		t.Fatalf("update TOTP: %v", err)
	}
	if err := store.TouchUserLogin(t.Context(), owner.ID, changedAt); err != nil {
		t.Fatalf("touch login: %v", err)
	}
	if err := store.DeleteUser(t.Context(), second.ID); err != nil {
		t.Fatalf("delete second owner: %v", err)
	}
	if err := store.UpdatePassword(t.Context(), "missing", "hash", changedAt); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("missing password update error = %v", err)
	}
}

func TestAdminStoreSessionsAndAudit(t *testing.T) {
	store := newAdminTestStore(t)
	owner := testAdminUser("owner", model.AdminRoleOwner)
	if err := store.CreateUser(t.Context(), &owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	now := model.NowUTC()
	session := model.AdminSession{
		ID: "session", UserID: owner.ID, TokenHash: []byte("hash"), TokenPrefix: "prefix",
		CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour), IPAddress: "127.0.0.1", UserAgent: "test",
	}
	if err := store.CreateSession(t.Context(), &session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	loaded, err := store.GetSessionByTokenHash(t.Context(), []byte("hash"))
	if err != nil || loaded == nil || !bytes.Equal(loaded.TokenHash, []byte("hash")) {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
	until := now.Add(10 * time.Minute)
	if err := store.MarkSessionReauthenticated(t.Context(), session.ID, until); err != nil {
		t.Fatalf("mark reauthenticated: %v", err)
	}
	if err := store.RevokeSession(t.Context(), session.ID, now); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	second := session
	second.ID = "session-2"
	second.TokenHash = []byte("hash-2")
	if err := store.CreateSession(t.Context(), &second); err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if loaded, err := store.GetSessionByID(t.Context(), second.ID); err != nil || loaded == nil {
		t.Fatalf("session by ID = %#v, %v", loaded, err)
	}
	sessions, err := store.ListSessions(t.Context(), owner.ID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions = %#v, %v", sessions, err)
	}
	if err := store.TouchSession(t.Context(), second.ID, now.Add(time.Minute), now.Add(2*time.Hour)); err != nil {
		t.Fatalf("touch session: %v", err)
	}
	third := session
	third.ID = "session-3"
	third.TokenHash = []byte("hash-3")
	if err := store.CreateSession(t.Context(), &third); err != nil {
		t.Fatalf("create third session: %v", err)
	}
	if count, err := store.RevokeOtherSessions(t.Context(), owner.ID, second.ID, now); err != nil || count != 1 {
		t.Fatalf("revoke other sessions = %d, %v", count, err)
	}
	if err := store.RevokeUserSessions(t.Context(), owner.ID, second.ID, now); err != nil {
		t.Fatalf("revoke user sessions: %v", err)
	}
	if count, err := store.DeleteExpiredSessions(t.Context(), now.Add(40*24*time.Hour)); err != nil || count == 0 {
		t.Fatalf("delete expired sessions = %d, %v", count, err)
	}
	event := model.AdminAuditEvent{
		ID: "event", ActorID: owner.ID, ActorUsername: owner.Username, Action: "session.revoke",
		ResourceType: "session", ResourceID: session.ID, Outcome: model.AuditOutcomeSuccess,
		DetailJSON: `{"reason":"test"}`, CreatedAt: now,
	}
	if err := store.CreateAuditEvent(t.Context(), &event); err != nil {
		t.Fatalf("create audit event: %v", err)
	}
	page, err := store.ListAuditEvents(t.Context(), model.AuditFilter{Limit: 25})
	if err != nil || len(page.Events) != 1 || page.Events[0].Action != event.Action {
		t.Fatalf("page = %#v, err = %v", page, err)
	}
	page, err = store.ListAuditEvents(t.Context(), model.AuditFilter{Action: event.Action,
		ActorID: owner.ID, Limit: 500, Offset: -1})
	if err != nil || page.Limit != 200 || page.Offset != 0 || page.Total != 1 {
		t.Fatalf("filtered audit page = %#v, %v", page, err)
	}
	if count, err := store.CleanupAuditEventsBefore(t.Context(), now.Add(time.Second)); err != nil || count != 1 {
		t.Fatalf("cleanup audit events = %d, %v", count, err)
	}
}

func TestAdminStoreBootstrapIsAtomicAndSingleUse(t *testing.T) {
	adminStore := newAdminTestStore(t)
	now := model.NowUTC()
	owner := testAdminUser("bootstrap-owner", model.AdminRoleOwner)
	session := model.AdminSession{
		ID: "bootstrap-session", UserID: owner.ID, TokenHash: []byte("bootstrap-hash"),
		TokenPrefix: "prefix", CreatedAt: now, LastActivityAt: now,
		IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}
	if err := adminStore.Bootstrap(t.Context(), &owner, &session); err != nil {
		t.Fatalf("bootstrap administrator: %v", err)
	}
	if err := adminStore.Bootstrap(t.Context(), &owner, &session); err == nil {
		t.Fatal("second administrator bootstrap succeeded")
	}
	loaded, err := adminStore.GetSessionByID(t.Context(), session.ID)
	if err != nil || loaded == nil || loaded.UserID != owner.ID {
		t.Fatalf("bootstrap session = %#v, %v", loaded, err)
	}

	rollbackSQLite, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatalf("new rollback store: %v", err)
	}
	t.Cleanup(func() { _ = rollbackSQLite.Close() })
	rollbackStore := NewAdminStore(rollbackSQLite.DB)
	invalidOwner := testAdminUser("invalid-owner", model.AdminRole("invalid"))
	invalidSession := session
	invalidSession.ID = "invalid-session"
	invalidSession.UserID = invalidOwner.ID
	invalidSession.TokenHash = []byte("invalid-hash")
	if err := rollbackStore.Bootstrap(t.Context(), &invalidOwner, &invalidSession); err == nil {
		t.Fatal("bootstrap with invalid owner role succeeded")
	}
	if count, err := rollbackStore.CountUsers(t.Context()); err != nil || count != 0 {
		t.Fatalf("failed bootstrap user count = %d, %v", count, err)
	}
}

func TestAdminModelsDoNotMarshalSecrets(t *testing.T) {
	user := testAdminUser("owner", model.AdminRoleOwner)
	encoded := user.String()
	for _, secret := range []string{user.PasswordHash, string(user.TOTPSecretEncrypted)} {
		if secret != "" && strings.Contains(encoded, secret) {
			t.Fatalf("serialized user contains secret %q", secret)
		}
	}
}

func newAdminTestStore(t *testing.T) *AdminStore {
	t.Helper()
	sqlite, err := NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return NewAdminStore(sqlite.DB)
}

func testAdminUser(username string, role model.AdminRole) model.AdminUser {
	now := model.NowUTC()
	return model.AdminUser{
		ID: username, Username: username, DisplayName: username, Role: role, PasswordHash: "encoded-hash",
		Enabled: true, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
		TOTPSecretEncrypted: []byte("ciphertext"),
	}
}
