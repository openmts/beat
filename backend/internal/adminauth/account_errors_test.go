package adminauth

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestAccountAuthorizationAndValidationErrors(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()
	adminPrincipal := &model.AdminPrincipal{
		User: model.AdminUser{ID: "admin", Username: "operator", Role: model.AdminRoleAdmin, Enabled: true},
	}
	owner := recentOwnerPrincipal()

	if _, err := service.ListUsers(ctx, adminPrincipal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("administrator list users error = %v", err)
	}
	if err := service.DeleteUser(ctx, adminPrincipal, "other"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("administrator delete user error = %v", err)
	}
	if _, err := service.ListUsers(ctx, adminPrincipal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("administrator list users error = %v", err)
	}
	if _, err := service.CreateUser(ctx, &owner, CreateUserRequest{
		Username: "!!!", DisplayName: "Bad", Role: model.AdminRoleAdmin,
		Password: "valid password here",
	}); err == nil {
		t.Fatal("create user accepted invalid username")
	}
	if err := service.UpdateUser(ctx, &owner, "other", UpdateUserRequest{
		DisplayName: "Valid", Role: "not-a-role", Enabled: true,
	}); err == nil {
		t.Fatal("update user accepted invalid role")
	}
}

func TestChangePasswordClosedStoreErrors(t *testing.T) {
	service := newClosedAuthService(t)
	principal := recentOwnerPrincipal()
	if err := service.ChangePassword(t.Context(), &principal, "current password long",
		"replacement password long", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("change password closed store error = %v", err)
	}
	if err := service.ChangePassword(t.Context(), &principal, "current", "short", ""); err == nil {
		t.Fatal("change password accepted a short replacement on a closed store")
	}
}

func TestPasswordHasherRandomSourceFailure(t *testing.T) {
	hasher := NewPasswordHasher(PasswordParams{
		MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, authFailingReader{})
	if _, err := hasher.Hash("password"); err == nil {
		t.Fatal("hash with failed random source succeeded")
	}
}

func TestRateLimiterEdgeCases(t *testing.T) {
	limiter := NewRateLimiter(2)
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	if !limiter.Allowed([]string{"ip:a"}, now) {
		t.Fatal("initial request not allowed")
	}
	for index := 0; index < loginFailures; index++ {
		limiter.Failure([]string{"ip:a"}, now.Add(time.Duration(index)*time.Minute))
	}
	if limiter.Allowed([]string{"ip:a"}, now.Add(10*time.Minute)) {
		t.Fatal("locked key still allowed")
	}
	if !limiter.Allowed([]string{"ip:a"}, now.Add(30*time.Minute)) {
		t.Fatal("expired lockout still blocking")
	}
	limiter.Success([]string{"ip:a"})
	if !limiter.Allowed([]string{"ip:a"}, now.Add(31*time.Minute)) {
		t.Fatal("successful login did not clear the entry")
	}
	pruned := NewRateLimiter(1)
	for index := 0; index < 4; index++ {
		pruned.Failure([]string{"ip:" + string(rune('a'+index))}, now.Add(time.Duration(index)*time.Second))
	}
	if len(pruned.entries) > pruned.maxEntries {
		t.Fatalf("pruned limiter entries = %d, want at most %d", len(pruned.entries), pruned.maxEntries)
	}
}

func TestRateLimiterPrunesExpiredEntries(t *testing.T) {
	limiter := NewRateLimiter(64)
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	limiter.Failure([]string{"ip:expired"}, now)
	if len(limiter.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(limiter.entries))
	}
	limiter.Failure([]string{"ip:expired"}, now.Add(3*loginWindow))
	if len(limiter.entries) != 1 {
		t.Fatalf("entries after pruning = %d, want 1", len(limiter.entries))
	}
	if _, ok := limiter.entries["ip:expired"]; !ok {
		t.Fatal("freshly updated key was pruned")
	}
}

func TestDefaultSQLiteStoreAndMemory(t *testing.T) {
	if _, err := store.NewSQLiteStore("file:beat-" + t.Name() + "?mode=memory&cache=shared"); err != nil {
		t.Fatalf("new memory sqlite: %v", err)
	}
	var buffer bytes.Buffer
	hasher := NewPasswordHasher(PasswordParams{
		MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, &buffer)
	if hasher.random != &buffer {
		t.Fatal("password hasher did not retain the supplied random source")
	}
}
