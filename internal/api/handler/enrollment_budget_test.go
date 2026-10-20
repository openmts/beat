package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnrollmentBudgetBoundsConcurrencyAndWindows(t *testing.T) {
	budget := newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 2, total: 3, concurrent: 1, keys: 2})
	now := time.Now()
	release, ok := budget.acquire("first", now)
	if !ok {
		t.Fatal("first request denied")
	}
	if _, ok := budget.acquire("second", now); ok {
		t.Fatal("concurrency limit bypassed")
	}
	release()
	release()
	release, ok = budget.acquire("first", now)
	if !ok {
		t.Fatal("second request denied")
	}
	release()
	if _, ok := budget.acquire("first", now); ok {
		t.Fatal("per-IP limit bypassed")
	}
	release, ok = budget.acquire("second", now)
	if !ok {
		t.Fatal("other address denied")
	}
	release()
	if _, ok := budget.acquire("third", now); ok {
		t.Fatal("global limit bypassed")
	}
	release, ok = budget.acquire("first", now.Add(time.Minute))
	if !ok {
		t.Fatal("window did not reset")
	}
	release()
}

func TestEnrollmentBudgetBoundsKeyStorage(t *testing.T) {
	budget := newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 3, total: 10, concurrent: 2, keys: 1})
	now := time.Now()
	release, ok := budget.acquire("first", now)
	if !ok {
		t.Fatal("first key denied")
	}
	release()
	if _, ok := budget.acquire("second", now); ok {
		t.Fatal("unbounded key storage")
	}
}

func TestEnrollmentAdmissionRejectsInvalidSourceAndSetsRetry(t *testing.T) {
	handler := NewAgentEnrollmentHandler(enrollmentStoreStub{})
	request := enrollmentHTTPRequest(`{}`, false)
	request.RemoteAddr = "invalid source"
	response := httptest.NewRecorder()
	if _, _, ok := handler.admit(response, request, handler.joinBudget); ok || response.Code != 400 {
		t.Fatal("invalid source admitted")
	}
	handler.joinBudget = newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 1, total: 1, concurrent: 1, keys: 1})
	request.RemoteAddr = "192.0.2.1:1234"
	response = httptest.NewRecorder()
	release, _, ok := handler.admit(response, request, handler.joinBudget)
	if !ok {
		t.Fatal("valid source denied")
	}
	release()
	response = httptest.NewRecorder()
	if _, _, ok := handler.admit(response, request, handler.joinBudget); ok || response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatal("rate limit response missing")
	}
}
