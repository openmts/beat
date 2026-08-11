package adminauth

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestServiceAdministratorLifecycleAndSecurityControls(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()
	if allowed, err := service.LegacyAuthorize(ctx, "bootstrap-secret"); err != nil || !allowed {
		t.Fatalf("legacy authorize before bootstrap = %v, %v", allowed, err)
	}
	if _, err := service.Bootstrap(ctx, BootstrapRequest{BootstrapToken: "wrong"}); !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("wrong bootstrap token error = %v", err)
	}
	owner, err := service.Bootstrap(ctx, BootstrapRequest{BootstrapToken: "bootstrap-secret",
		Username: "owner", DisplayName: "Owner", Password: "correct horse battery staple",
		IPAddress: "127.0.0.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	if allowed, err := service.LegacyAuthorize(ctx, "bootstrap-secret"); err != nil || allowed {
		t.Fatalf("legacy authorize after bootstrap = %v, %v", allowed, err)
	}
	if _, err := service.Bootstrap(ctx, BootstrapRequest{}); !errors.Is(err, ErrBootstrapComplete) {
		t.Fatalf("second bootstrap error = %v", err)
	}
	users, err := service.ListUsers(ctx, &owner.Principal)
	if err != nil || len(users) != 1 {
		t.Fatalf("list users = %#v, %v", users, err)
	}
	admin, err := service.CreateUser(ctx, &owner.Principal, CreateUserRequest{Username: "operator",
		DisplayName: "Operator", Role: model.AdminRoleAdmin, Password: "operator password long"})
	if err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	if _, err := service.CreateUser(ctx, &owner.Principal, CreateUserRequest{Username: "bad",
		DisplayName: "Bad", Role: model.AdminRoleAdmin, Password: "short"}); err == nil {
		t.Fatal("short administrator password accepted")
	}
	if err := service.UpdateUser(ctx, &owner.Principal, admin.ID, UpdateUserRequest{
		DisplayName: "Updated", Role: model.AdminRoleAdmin, Enabled: true,
	}); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("update without recent authentication error = %v", err)
	}
	if err := service.Reauthenticate(ctx, &owner.Principal, "correct horse battery staple", ""); err != nil {
		t.Fatalf("reauthenticate owner: %v", err)
	}
	if err := service.UpdateUser(ctx, &owner.Principal, admin.ID, UpdateUserRequest{
		DisplayName: "Updated", Role: model.AdminRoleAdmin, Enabled: true,
	}); err != nil {
		t.Fatalf("update administrator: %v", err)
	}
	if err := service.DeleteUser(ctx, &owner.Principal, owner.Principal.User.ID); err == nil {
		t.Fatal("current owner deleted their own account")
	}

	adminLogin, err := service.Login(ctx, LoginRequest{Username: "operator",
		Password: "operator password long", IPAddress: "127.0.0.2", UserAgent: "operator"})
	if err != nil {
		t.Fatalf("login administrator: %v", err)
	}
	if err := service.RequireOwner(&adminLogin.Principal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("administrator owner check error = %v", err)
	}
	if err := service.RequireOwner(nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nil owner check error = %v", err)
	}
	sessions, err := service.ListSessions(ctx, &adminLogin.Principal)
	if err != nil || len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("administrator sessions = %#v, %v", sessions, err)
	}
	if err := service.RevokeSession(ctx, &owner.Principal, adminLogin.Principal.Session.ID); err == nil {
		t.Fatal("owner revoked another user's session through self-session API")
	}
	if err := service.RevokeSession(ctx, &adminLogin.Principal, adminLogin.Principal.Session.ID); err != nil {
		t.Fatalf("revoke current administrator session: %v", err)
	}
	if _, err := service.Authenticate(ctx, adminLogin.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session authentication error = %v", err)
	}

	secondOwner, err := service.CreateUser(ctx, &owner.Principal, CreateUserRequest{Username: "backup-owner",
		DisplayName: "Backup Owner", Role: model.AdminRoleOwner, Password: "backup owner password"})
	if err != nil {
		t.Fatalf("create second owner: %v", err)
	}
	if err := service.DeleteUser(ctx, &owner.Principal, secondOwner.ID); err != nil {
		t.Fatalf("delete second owner: %v", err)
	}
	if err := service.DeleteUser(ctx, &owner.Principal, admin.ID); err != nil {
		t.Fatalf("delete administrator: %v", err)
	}

	secondSession, err := service.Login(ctx, LoginRequest{Username: "owner",
		Password: "correct horse battery staple", IPAddress: "127.0.0.3"})
	if err != nil {
		t.Fatalf("create second owner session: %v", err)
	}
	revoked, err := service.RevokeOtherSessions(ctx, &owner.Principal)
	if err != nil || revoked != 1 {
		t.Fatalf("revoke other sessions = %d, %v", revoked, err)
	}
	if _, err := service.Authenticate(ctx, secondSession.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("other session remains active: %v", err)
	}
	if err := service.ChangePassword(ctx, &owner.Principal, "correct horse battery staple",
		"replacement horse battery staple", ""); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := service.Login(ctx, LoginRequest{Username: "owner",
		Password: "replacement horse battery staple", IPAddress: "127.0.0.4"}); err != nil {
		t.Fatalf("login with replacement password: %v", err)
	}

	setup, err := service.BeginTOTP(ctx, &owner.Principal)
	if err != nil {
		t.Fatalf("begin TOTP: %v", err)
	}
	if err := service.EnableTOTP(ctx, &owner.Principal, "000000"); !errors.Is(err, ErrInvalidTOTP) {
		t.Fatalf("invalid TOTP enable error = %v", err)
	}
	code, err := totp.GenerateCode(setup.Secret, service.now())
	if err != nil {
		t.Fatalf("generate TOTP: %v", err)
	}
	if err := service.EnableTOTP(ctx, &owner.Principal, code); err != nil {
		t.Fatalf("enable TOTP: %v", err)
	}
	owner.Principal.Session.ReauthenticatedUntil = nil
	if err := service.DisableTOTP(ctx, &owner.Principal); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("disable TOTP without recent authentication error = %v", err)
	}
	code, _ = totp.GenerateCode(setup.Secret, service.now())
	if err := service.Reauthenticate(ctx, &owner.Principal, "replacement horse battery staple", code); err != nil {
		t.Fatalf("reauthenticate with TOTP: %v", err)
	}
	if err := service.DisableTOTP(ctx, &owner.Principal); err != nil {
		t.Fatalf("disable TOTP: %v", err)
	}
	page, err := service.ListAuditEvents(ctx, &owner.Principal, model.AuditFilter{Limit: 200})
	if err != nil || len(page.Events) == 0 {
		t.Fatalf("audit page = %#v, %v", page, err)
	}
	if _, err := service.ListAuditEvents(ctx, nil, model.AuditFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nil audit principal error = %v", err)
	}
	if err := service.Logout(ctx, &owner.Principal); err != nil {
		t.Fatalf("logout owner: %v", err)
	}
}

func TestServiceLoginRateLimitAndSessionCleanup(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()
	_, err := service.Bootstrap(ctx, BootstrapRequest{BootstrapToken: "bootstrap-secret",
		Username: "owner", DisplayName: "Owner", Password: "correct horse battery staple"})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		_, _ = service.Login(ctx, LoginRequest{Username: "owner", Password: "wrong", IPAddress: "locked"})
	}
	if _, err := service.Login(ctx, LoginRequest{Username: "owner", Password: "wrong", IPAddress: "locked"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("rate limited login error = %v", err)
	}
	if _, err := service.config.Store.DeleteExpiredSessions(ctx, service.now().Add(8*24*time.Hour)); err != nil {
		t.Fatalf("delete expired sessions: %v", err)
	}
}

func TestServiceBootstrapLoginAndSession(t *testing.T) {
	service := newTestService(t)
	result, err := service.Bootstrap(t.Context(), BootstrapRequest{
		BootstrapToken: "bootstrap-secret", Username: "Owner", DisplayName: "Primary owner",
		Password: "correct horse battery staple", IPAddress: "127.0.0.1", UserAgent: "test",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if result.Token == "" || result.Principal.User.Role != model.AdminRoleOwner {
		t.Fatalf("bootstrap result = %#v", result)
	}
	state, err := service.State(t.Context())
	if err != nil || state.SetupRequired {
		t.Fatalf("state = %#v, err = %v", state, err)
	}
	principal, err := service.Authenticate(t.Context(), result.Token)
	if err != nil || principal.User.ID != result.Principal.User.ID {
		t.Fatalf("principal = %#v, err = %v", principal, err)
	}
	if _, err := service.Login(t.Context(), LoginRequest{
		Username: "owner", Password: "wrong password", IPAddress: "127.0.0.1",
	}); err != ErrInvalidCredentials {
		t.Fatalf("wrong password error = %v", err)
	}
	login, err := service.Login(t.Context(), LoginRequest{
		Username: "owner", Password: "correct horse battery staple", IPAddress: "127.0.0.1",
	})
	if err != nil || login.Token == "" {
		t.Fatalf("login = %#v, err = %v", login, err)
	}
}

func TestServiceTOTPAndReauthentication(t *testing.T) {
	service := newTestService(t)
	result, err := service.Bootstrap(t.Context(), BootstrapRequest{
		BootstrapToken: "bootstrap-secret", Username: "owner", DisplayName: "Owner",
		Password: "correct horse battery staple", IPAddress: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	setup, err := service.BeginTOTP(t.Context(), &result.Principal)
	if err != nil || setup.Secret == "" || setup.URI == "" {
		t.Fatalf("TOTP setup = %#v, err = %v", setup, err)
	}
	code, err := totp.GenerateCode(setup.Secret, service.now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	if err := service.EnableTOTP(t.Context(), &result.Principal, code); err != nil {
		t.Fatalf("enable TOTP: %v", err)
	}
	if _, err := service.Login(t.Context(), LoginRequest{
		Username: "owner", Password: "correct horse battery staple", IPAddress: "127.0.0.2",
	}); err != ErrTOTPRequired {
		t.Fatalf("login without TOTP error = %v", err)
	}
	code, _ = totp.GenerateCode(setup.Secret, service.now())
	login, err := service.Login(t.Context(), LoginRequest{
		Username: "owner", Password: "correct horse battery staple", TOTPCode: code,
		IPAddress: "127.0.0.2",
	})
	if err != nil {
		t.Fatalf("login with TOTP: %v", err)
	}
	code, _ = totp.GenerateCode(setup.Secret, service.now())
	if err := service.Reauthenticate(t.Context(), &login.Principal,
		"correct horse battery staple", code); err != nil {
		t.Fatalf("reauthenticate: %v", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	randomBytes := make([]byte, 4096)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	random := bytes.NewReader(randomBytes)
	secrets, err := secretbox.New(filepath.Join(t.TempDir(), "admin-data.key"), random)
	if err != nil {
		t.Fatalf("new secret box: %v", err)
	}
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	service, err := NewService(ServiceConfig{
		Store: store.NewAdminStore(sqlite.DB), Secrets: secrets, LegacyToken: "bootstrap-secret",
		Passwords: NewPasswordHasher(PasswordParams{MemoryKiB: 64, Iterations: 1,
			Parallelism: 1, SaltLength: 16, KeyLength: 32}, random),
		Random: random, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new authentication service: %v", err)
	}
	return service
}
