package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/alerteval"
)

func TestAlertScopeCapacityResponsePreservesRetrySemantics(t *testing.T) {
	for _, failure := range []error{nil, errors.New("other")} {
		if respondAlertScopeCapacity(httptest.NewRecorder(), failure) {
			t.Fatal("unrelated error captured")
		}
	}
	response := httptest.NewRecorder()
	if !respondAlertScopeCapacity(response, alerteval.ErrCapacity) || response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "30" {
		t.Fatal(response.Code, response.Header())
	}
	response = httptest.NewRecorder()
	enrollmentJoinError(response, alerteval.ErrCapacity)
	if response.Code != http.StatusTooManyRequests {
		t.Fatal(response.Code)
	}
}
