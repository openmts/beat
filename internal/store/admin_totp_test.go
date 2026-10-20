package store

import (
	"bytes"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func totpActivationFixture(t *testing.T) (*AdminStore, TOTPActivation) {
	t.Helper()
	store := newAdminTestStore(t)
	user := testAdminUser("owner", model.AdminRoleOwner)
	now := model.NowUTC()
	user.TOTPEnabledAt = &now
	if err := store.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Hour)
	session := model.AdminSession{ID: "session", UserID: user.ID, TokenHash: []byte("hash"),
		CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until,
		ReauthenticatedUntil: &until}
	if err := store.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return store, TOTPActivation{UserID: user.ID, SessionID: session.ID, Secret: []byte("new-ciphertext"),
		PreviousSecret: user.TOTPSecretEncrypted, PreviousEnabledAt: user.TOTPEnabledAt, Now: now}
}

func TestTOTPActivationChecksFactorAndSessionAtomically(t *testing.T) {
	for _, mutation := range []string{"factor", "enabled-at", "revoked", "expired", "recent", "user", "disabled"} {
		t.Run(mutation, func(t *testing.T) {
			store, input := totpActivationFixture(t)
			switch mutation {
			case "factor":
				input.PreviousSecret = []byte("stale")
			case "enabled-at":
				input.PreviousEnabledAt = nil
			case "revoked":
				if err := store.RevokeSession(t.Context(), input.SessionID, input.Now); err != nil {
					t.Fatal(err)
				}
			case "expired":
				input.Now = input.Now.Add(time.Hour)
			case "recent":
				if err := store.MarkSessionReauthenticated(t.Context(), input.SessionID, input.Now); err != nil {
					t.Fatal(err)
				}
			case "user":
				input.UserID = "missing"
			case "disabled":
				if _, err := store.db.Exec("UPDATE admin_users SET enabled=0"); err != nil {
					t.Fatal(err)
				}
			}
			confirmed, err := store.ConfirmTOTP(t.Context(), input)
			if err != nil || confirmed {
				t.Fatalf("rejected activation = %v, %v", confirmed, err)
			}
			user, err := store.GetUserByID(t.Context(), "owner")
			if err != nil || !bytes.Equal(user.TOTPSecretEncrypted, []byte("ciphertext")) {
				t.Fatalf("original factor changed: %v", err)
			}
		})
	}
}

func TestTOTPActivationHandlesValidationAndWriteFailure(t *testing.T) {
	store, input := totpActivationFixture(t)
	if _, err := store.ConfirmTOTP(t.Context(), TOTPActivation{}); err == nil {
		t.Fatal("invalid activation accepted")
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_totp BEFORE UPDATE ON admin_users
		BEGIN SELECT RAISE(ABORT, 'write failed'); END`); err != nil {
		t.Fatal(err)
	}
	if confirmed, err := store.ConfirmTOTP(t.Context(), input); err == nil || confirmed {
		t.Fatal("write failure accepted")
	}
	user, err := store.GetUserByID(t.Context(), input.UserID)
	if err != nil || !bytes.Equal(user.TOTPSecretEncrypted, input.PreviousSecret) {
		t.Fatalf("failed write changed factor: %v", err)
	}
}
