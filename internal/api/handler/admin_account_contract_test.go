package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestAdministratorMutationErrorContract(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{store.ErrAdminUsernameConflict, http.StatusConflict},
		{store.ErrAdminMutationConflict, http.StatusConflict},
		{errors.Join(store.ErrAdminMutationUnavailable, errors.New("private SQLite path and password hash")), http.StatusInternalServerError},
	} {
		response := httptest.NewRecorder()
		writeSecurityError(response, test.err)
		if response.Code != test.status {
			t.Fatalf("status=%d, want=%d", response.Code, test.status)
		}
		if strings.Contains(response.Body.String(), "private SQLite") || strings.Contains(response.Body.String(), "password hash") {
			t.Fatal("internal failure detail disclosed")
		}
	}
}
