package middleware

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestWithAdminPrincipalRoundTrip(t *testing.T) {
	principal := model.AdminPrincipal{
		User: model.AdminUser{ID: "user-1", Username: "owner"},
	}
	ctx := WithAdminPrincipal(context.Background(), principal)
	got, ok := AdminPrincipal(ctx)
	if !ok {
		t.Fatal("AdminPrincipal did not find a principal")
	}
	if got.User.ID != "user-1" || got.User.Username != "owner" {
		t.Fatalf("principal = %+v, want user-1/owner", got)
	}
}

func TestAdminPrincipalMissing(t *testing.T) {
	if _, ok := AdminPrincipal(context.Background()); ok {
		t.Fatal("AdminPrincipal returned ok for a bare context")
	}
}

func TestAdminPrincipalWrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), adminPrincipalKey{}, "not-a-principal")
	if _, ok := AdminPrincipal(ctx); ok {
		t.Fatal("AdminPrincipal accepted a non-principal value")
	}
}
