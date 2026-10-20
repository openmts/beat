package handler

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func seedThemeOwner(t *testing.T, database *store.SQLiteStore) {
	t.Helper()
	users := store.NewAdminStore(database.DB)
	now, expiry := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
	user := model.AdminUser{ID: "owner-1", Username: "theme-owner", DisplayName: "Theme owner", Role: model.AdminRoleOwner,
		Enabled: true, CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now}
	if err := users.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: "s-1", UserID: user.ID, TokenHash: make([]byte, 32), TokenPrefix: "theme-fixture",
		CreatedAt: now, LastActivityAt: now, IdleExpiresAt: expiry, AbsoluteExpiresAt: expiry,
		ReauthenticatedUntil: &expiry, AuthenticationMethod: "local"}
	if err := users.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
}
