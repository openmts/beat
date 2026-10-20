package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestMetadataMutationErrorStatuses(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{store.ErrMetadataForbidden, http.StatusForbidden}, {store.ErrMetadataRecentAuth, http.StatusPreconditionRequired}} {
		response := httptest.NewRecorder()
		if !respondMetadataMutationError(response, test.err) || response.Code != test.status {
			t.Fatal(response.Code)
		}
	}
	if respondMetadataMutationError(httptest.NewRecorder(), errors.New("other")) {
		t.Fatal("unrelated error captured")
	}
}
