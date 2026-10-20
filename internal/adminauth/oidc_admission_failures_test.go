package adminauth

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"testing"

	"github.com/beat/backend/internal/model"
)

type oidcTokenReader func([]byte) (int, error)

func (reader oidcTokenReader) Read(destination []byte) (int, error) { return reader(destination) }

func TestOIDCSessionAdmissionRejectsChangesAfterUserRead(t *testing.T) {
	for _, query := range []string{"UPDATE admin_users SET enabled=0", "DELETE FROM admin_external_identities", "UPDATE admin_oidc_providers SET configuration_version=2"} {
		t.Run(query, func(t *testing.T) {
			service, input, db := newOIDCAuthFixture(t)
			service.random = oidcTokenReader(func(destination []byte) (int, error) {
				if _, err := db.Exec(query); err != nil {
					return 0, err
				}
				return bytes.NewReader(bytes.Repeat([]byte{4}, len(destination))).Read(destination)
			})
			result, err := service.CreateOIDCSession(t.Context(), input)
			if !errors.Is(err, ErrInvalidCredentials) || result.Token != "" {
				t.Fatal("stale admission returned token", err)
			}
			assertNoAuthOIDCSession(t, db)
		})
	}
}

func TestOIDCSessionCreationFailureReturnsNoToken(t *testing.T) {
	for _, kind := range []string{"random", "audit", "query"} {
		t.Run(kind, func(t *testing.T) {
			service, input, db := newOIDCAuthFixture(t)
			switch kind {
			case "random":
				service.random = oidcTokenReader(func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF })
			case "audit":
				if _, err := db.Exec(`CREATE TRIGGER failed_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'full'); END`); err != nil {
					t.Fatal(err)
				}
			case "query":
				if _, err := db.Exec("ALTER TABLE admin_users RENAME TO unavailable_users"); err != nil {
					t.Fatal(err)
				}
			}
			result, err := service.CreateOIDCSession(t.Context(), input)
			if err == nil || result.Token != "" {
				t.Fatal("failed admission returned token", err)
			}
			assertNoAuthOIDCSession(t, db)
		})
	}
}

func newOIDCAuthFixture(t *testing.T) (*Service, OIDCSessionRequest, *sql.DB) {
	t.Helper()
	service := newTestService(t)
	now := service.now()
	user := model.AdminUser{ID: "oidc-user", Username: "oidc-user", DisplayName: "OIDC", Role: model.AdminRoleAdmin, Enabled: true, PasswordHash: "hash", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := service.config.Store.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	input := oidcSessionTestRequest(t, service, user.ID, "identity")
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return service, input, db
}

func assertNoAuthOIDCSession(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial session", count, err)
	}
}
