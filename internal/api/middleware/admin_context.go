package middleware

import (
	"context"

	"github.com/beat/backend/internal/model"
)

type adminPrincipalKey struct{}

func WithAdminPrincipal(ctx context.Context, principal model.AdminPrincipal) context.Context {
	return context.WithValue(ctx, adminPrincipalKey{}, principal)
}

func AdminPrincipal(ctx context.Context) (model.AdminPrincipal, bool) {
	principal, ok := ctx.Value(adminPrincipalKey{}).(model.AdminPrincipal)
	return principal, ok
}
