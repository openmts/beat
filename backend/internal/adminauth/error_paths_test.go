package adminauth

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestNewServiceDefaultsAndErrors(t *testing.T) {
	if _, err := NewService(ServiceConfig{}); err == nil {
		t.Fatal("authentication service accepted missing dependencies")
	}
	sqliteStore, secrets := authDependencies(t)
	invalidHasher := NewPasswordHasher(PasswordParams{
		MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, authFailingReader{})
	if _, err := NewService(ServiceConfig{
		Store: store.NewAdminStore(sqliteStore.DB), Secrets: secrets, Passwords: invalidHasher,
	}); err == nil {
		t.Fatal("authentication service ignored dummy hash failure")
	}
	validHasher := NewPasswordHasher(PasswordParams{
		MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, bytes.NewReader(bytes.Repeat([]byte{3}, 64)))
	service, err := NewService(ServiceConfig{
		Store: store.NewAdminStore(sqliteStore.DB), Secrets: secrets, Passwords: validHasher,
	})
	if err != nil || service.random == nil || service.now == nil {
		t.Fatalf("authentication defaults = %#v, %v", service, err)
	}
}

func TestServiceClosedStoreErrors(t *testing.T) {
	service := newClosedAuthService(t)
	principal := recentOwnerPrincipal()
	ctx := t.Context()
	if _, err := service.State(ctx); err == nil {
		t.Fatal("authentication state ignored closed store")
	}
	if _, err := service.LegacyAuthorize(ctx, "token"); err == nil {
		t.Fatal("legacy authorization ignored closed store")
	}
	if err := service.Logout(ctx, &principal); err == nil {
		t.Fatal("logout ignored closed store")
	}
	if _, err := service.Bootstrap(ctx, BootstrapRequest{}); err == nil {
		t.Fatal("bootstrap ignored closed store")
	}
	if _, err := service.Login(ctx, LoginRequest{Username: "owner"}); err == nil {
		t.Fatal("login ignored closed store")
	}
	if _, err := service.Authenticate(ctx, "token"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("authenticate error = %v", err)
	}
	assertClosedAccountErrors(t, service, &principal)
	assertClosedTOTPAndSessionErrors(t, service, &principal)
}

func TestServiceConstructionAndVerificationErrors(t *testing.T) {
	service := newTestService(t)
	if _, err := service.newBootstrapUser(BootstrapRequest{Password: "short"}); err == nil {
		t.Fatal("bootstrap user accepted short password")
	}
	if _, err := service.newBootstrapUser(BootstrapRequest{
		Password: "correct horse battery staple",
	}); err == nil {
		t.Fatal("bootstrap user accepted missing identity")
	}
	originalRandom := service.random
	service.random = authFailingReader{}
	if _, _, err := service.newSession(&model.AdminUser{}, "", ""); err == nil {
		t.Fatal("session generated with failed random source")
	}
	service.random = originalRandom
	if _, _, _, err := GenerateSessionToken(authFailingReader{}); err == nil {
		t.Fatal("session token generated with failed random source")
	}
	if SessionTokenMatches("token", []byte("short")) {
		t.Fatal("session token matched invalid hash length")
	}
	if err := service.verifyLogin(nil, LoginRequest{Password: "wrong"}, service.now()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("nil user login error = %v", err)
	}
	hash, err := service.config.Passwords.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash verification password: %v", err)
	}
	disabled := &model.AdminUser{Enabled: false, PasswordHash: hash}
	if err := service.verifyLogin(disabled, LoginRequest{Password: "correct horse battery staple"}, service.now()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user login error = %v", err)
	}
	if minTime(time.Unix(2, 0), time.Unix(1, 0)) != time.Unix(1, 0) {
		t.Fatal("minimum time selected the later value")
	}
}

func assertClosedAccountErrors(t *testing.T, service *Service, principal *model.AdminPrincipal) {
	t.Helper()
	ctx := t.Context()
	if _, err := service.ListUsers(ctx, principal); err == nil {
		t.Fatal("list users ignored closed store")
	}
	if _, err := service.CreateUser(ctx, nil, CreateUserRequest{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create user permission error = %v", err)
	}
	if _, err := service.CreateUser(ctx, principal, CreateUserRequest{
		Username: "operator", DisplayName: "Operator", Role: model.AdminRoleAdmin,
		Password: "operator password long",
	}); err == nil {
		t.Fatal("create user ignored closed store")
	}
	if err := service.UpdateUser(ctx, principal, "other", UpdateUserRequest{
		DisplayName: "Operator", Role: model.AdminRoleAdmin, Enabled: true,
	}); err == nil {
		t.Fatal("update user ignored closed store")
	}
	if err := service.DeleteUser(ctx, principal, "other"); err == nil {
		t.Fatal("delete user ignored closed store")
	}
	if err := service.ChangePassword(ctx, principal, "current password long", "short", ""); err == nil {
		t.Fatal("change password accepted short replacement")
	}
}

func assertClosedTOTPAndSessionErrors(t *testing.T, service *Service, principal *model.AdminPrincipal) {
	t.Helper()
	ctx := t.Context()
	if _, err := service.BeginTOTP(ctx, principal); err == nil {
		t.Fatal("begin TOTP ignored closed store")
	}
	if err := service.EnableTOTP(ctx, principal, "000000"); err == nil {
		t.Fatal("enable TOTP ignored absent setup")
	}
	if err := service.DisableTOTP(ctx, principal); err == nil {
		t.Fatal("disable TOTP ignored closed store")
	}
	if _, err := service.ListSessions(ctx, principal); err == nil {
		t.Fatal("list sessions ignored closed store")
	}
	if err := service.RevokeSession(ctx, principal, "session"); err == nil {
		t.Fatal("revoke session ignored closed store")
	}
	if _, err := service.RevokeOtherSessions(ctx, principal); err == nil {
		t.Fatal("revoke other sessions ignored closed store")
	}
	if err := service.RecordAudit(ctx, AuditInput{}); err == nil {
		t.Fatal("audit write ignored closed store")
	}
	if _, err := service.ListAuditEvents(ctx, principal, model.AuditFilter{}); err == nil {
		t.Fatal("audit list ignored closed store")
	}
}

func newClosedAuthService(t *testing.T) *Service {
	t.Helper()
	sqliteStore, secrets := authDependencies(t)
	random := bytes.NewReader(bytes.Repeat([]byte{5}, 32768))
	service, err := NewService(ServiceConfig{
		Store: store.NewAdminStore(sqliteStore.DB), Secrets: secrets, LegacyToken: "token", Random: random,
		Passwords: NewPasswordHasher(PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, random),
	})
	if err != nil {
		t.Fatalf("new closed-store service: %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close authentication store: %v", err)
	}
	return service
}

func authDependencies(t *testing.T) (*store.SQLiteStore, *secretbox.Manager) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("new authentication store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	secrets, err := secretbox.New(filepath.Join(t.TempDir(), "admin-data.key"),
		bytes.NewReader(bytes.Repeat([]byte{4}, 64)))
	if err != nil {
		t.Fatalf("new authentication secrets: %v", err)
	}
	return sqliteStore, secrets
}

func recentOwnerPrincipal() model.AdminPrincipal {
	until := time.Now().UTC().Add(time.Hour)
	return model.AdminPrincipal{
		User:    model.AdminUser{ID: "owner", Username: "owner", Role: model.AdminRoleOwner, Enabled: true},
		Session: model.AdminSession{ID: "session", UserID: "owner", ReauthenticatedUntil: &until},
	}
}

type authFailingReader struct{}

func (authFailingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
