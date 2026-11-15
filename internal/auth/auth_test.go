package auth

import (
	"context"
	"strings"
	"testing"

	"beat/internal/platform/sqlite"
)

func newTestManager(t *testing.T) (*Manager, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	return manager, store
}

func TestDefaultCredentialsAndPasswordHash(t *testing.T) {
	manager, store := newTestManager(t)
	ctx := context.Background()

	credentials, valid, err := manager.Authenticate(ctx, "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !valid || credentials.Username != "admin" {
		t.Fatalf("default admin/admin authentication failed: valid=%v credentials=%+v", valid, credentials)
	}
	if _, valid, err := manager.Authenticate(ctx, "admin", "wrong"); err != nil || valid {
		t.Fatalf("wrong password accepted: valid=%v err=%v", valid, err)
	}

	raw, ok, err := store.GetSetting(ctx, CredentialsKey)
	if err != nil || !ok {
		t.Fatalf("credentials setting missing: ok=%v err=%v", ok, err)
	}
	if strings.Contains(raw, `"password"`) || strings.Contains(raw, `"admin"`) && !strings.Contains(raw, `"username":"admin"`) {
		t.Fatalf("credentials setting contains plaintext credentials: %s", raw)
	}
}

func TestUpdateCredentialsInvalidatesExistingSessions(t *testing.T) {
	manager, _ := newTestManager(t)
	ctx := context.Background()
	credentials, valid, err := manager.Authenticate(ctx, "admin", "admin")
	if err != nil || !valid {
		t.Fatalf("default authentication failed: valid=%v err=%v", valid, err)
	}
	session, err := manager.IssueSession(credentials.Username, credentials.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, valid, err := manager.ValidateSession(ctx, session.Token); err != nil || !valid {
		t.Fatalf("issued session is invalid: valid=%v err=%v", valid, err)
	}

	if _, err := manager.UpdateCredentials(ctx, "admin", "ops-admin", "new-secret-123"); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := manager.ValidateSession(ctx, session.Token); err != nil || valid {
		t.Fatalf("old session remained valid after credential update: valid=%v err=%v", valid, err)
	}
	if _, valid, err := manager.Authenticate(ctx, "admin", "admin"); err != nil || valid {
		t.Fatalf("old credentials remained valid: valid=%v err=%v", valid, err)
	}
	if _, valid, err := manager.Authenticate(ctx, "ops-admin", "new-secret-123"); err != nil || !valid {
		t.Fatalf("new credentials rejected: valid=%v err=%v", valid, err)
	}
}

func TestResetCredentialsRestoresDefaultAndInvalidatesSessions(t *testing.T) {
	manager, store := newTestManager(t)
	ctx := context.Background()
	credentials, err := manager.UpdateCredentials(ctx, "admin", "operator", "not-the-default")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueSession(credentials.Username, credentials.Version)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ResetCredentials(ctx, store); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := manager.ValidateSession(ctx, session.Token); err != nil || valid {
		t.Fatalf("session remained valid after CLI reset: valid=%v err=%v", valid, err)
	}
	if _, valid, err := manager.Authenticate(ctx, DefaultUsername, DefaultPassword); err != nil || !valid {
		t.Fatalf("default credentials were not restored: valid=%v err=%v", valid, err)
	}
}

func TestPasswordValidation(t *testing.T) {
	if err := validatePassword("short"); err != ErrPasswordTooShort {
		t.Fatalf("validatePassword(short) = %v", err)
	}
	if err := validatePassword(strings.Repeat("x", 129)); err != ErrPasswordTooLong {
		t.Fatalf("validatePassword(129 chars) = %v", err)
	}
	if err := validatePassword("long-enough"); err != nil {
		t.Fatalf("validatePassword(valid) = %v", err)
	}
}
