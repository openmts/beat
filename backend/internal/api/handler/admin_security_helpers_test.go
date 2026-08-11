package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func newClosedHandlerSecurity(t *testing.T) *adminauth.Service {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("new closed SQLite store: %v", err)
	}
	random := bytes.NewReader(bytes.Repeat([]byte{8}, 32768))
	secrets, err := secretbox.New(filepath.Join(t.TempDir(), "admin-data.key"), random)
	if err != nil {
		t.Fatalf("new closed-store secret box: %v", err)
	}
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: store.NewAdminStore(sqliteStore.DB), Secrets: secrets, LegacyToken: "bootstrap", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, random),
	})
	if err != nil {
		_ = sqliteStore.Close()
		t.Fatalf("new closed-store security service: %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close SQLite store: %v", err)
	}
	return security
}

func newHandlerSecurity(t *testing.T) *adminauth.Service {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	random := bytes.NewReader(bytes.Repeat([]byte{9}, 16384))
	secrets, err := secretbox.New(filepath.Join(t.TempDir(), "admin-data.key"), random)
	if err != nil {
		t.Fatalf("new secret box: %v", err)
	}
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(sqliteStore.DB),
		Secrets: secrets, LegacyToken: "bootstrap", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{MemoryKiB: 64, Iterations: 1,
			Parallelism: 1, SaltLength: 16, KeyLength: 32}, random)})
	if err != nil {
		t.Fatalf("new security service: %v", err)
	}
	return security
}

func callAdminHandler(
	t *testing.T,
	principal *model.AdminPrincipal,
	method string,
	path string,
	body string,
	handle func(http.ResponseWriter, *http.Request),
) *httptest.ResponseRecorder {
	t.Helper()
	request := newAdminRequest(t, principal, method, path, body)
	response := httptest.NewRecorder()
	handle(response, request)
	return response
}

func newAdminRequest(
	t *testing.T,
	principal *model.AdminPrincipal,
	method string,
	path string,
	body string,
) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	request.RemoteAddr = "[::1]:1234"
	if principal != nil {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), *principal))
	}
	return request
}
