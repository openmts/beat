package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type metricAuthorityBody struct {
	io.Reader
	before func()
	once   sync.Once
}

func (body *metricAuthorityBody) Read(value []byte) (int, error) {
	body.once.Do(body.before)
	return body.Reader.Read(value)
}

func TestMetricCatalogRejectsAuthorityChangedAfterHTTPAuthentication(t *testing.T) {
	for _, operation := range []string{"revoke", "demote"} {
		t.Run(operation, func(t *testing.T) {
			router := setupMetricCatalogRouter(t)
			owner := boundaryOwner(t, router)
			sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlite.Close() })
			body := &metricAuthorityBody{Reader: strings.NewReader(`{"revision":1,"label":"Rejected","description":"","default_aggregation":"avg","public":false,"enabled":true,"retention_days":null}`)}
			body.before = func() {
				var err error
				if operation == "revoke" {
					_, err = sqlite.DB.Exec("UPDATE admin_sessions SET revoked_at=? WHERE id=?", model.NowUTC(), owner.Principal.Session.ID)
				} else {
					_, err = sqlite.DB.Exec("UPDATE admin_users SET role='admin' WHERE id=?", owner.Principal.User.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			request := httptest.NewRequest(http.MethodPut, "http://example.com/api/v1/admin/metrics/catalog/cpu", body)
			request.Header.Set("Origin", "http://example.com")
			request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: owner.Token})
			response := httptest.NewRecorder()
			router.ServeHandler().ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("stale HTTP proof status=%d", response.Code)
			}
			definition, err := router.metricCatalog.Get(t.Context(), "cpu")
			if err != nil || definition.Label == "Rejected" || definition.Revision != 1 {
				t.Fatal("post-authentication revoke/demotion committed policy")
			}
		})
	}
}
