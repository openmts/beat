package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicInteractionBudgetClampsAndExpires(t *testing.T) {
	budget := newPublicInteractionBudget(1)
	if expected, actual := 64, budget.maximum; expected != actual {
		t.Fatalf("maximum = %d, want %d", actual, expected)
	}
	now := time.Now().UTC()
	if !budget.Allow("a", now) {
		t.Fatal("first request must be allowed")
	}
	if !budget.Allow("a", now.Add(2*time.Minute)) {
		t.Fatal("a new window must allow the same source again")
	}
	seen := make(map[string]struct{}, 64)
	for index := 0; index < 64; index++ {
		key := string(rune('a'+index%26)) + string(rune('a'+index/26))
		seen[key] = struct{}{}
		_ = budget.Allow(key, now.Add(2*time.Minute))
	}
	if budget.Allow("overflow", now.Add(2*time.Minute+time.Second)) {
		t.Fatal("an exhausted budget must reject new anonymous keys")
	}
}

func TestPublicInteractionBudgetRejectsBeyondCeiling(t *testing.T) {
	budget := newPublicInteractionBudget(64)
	now := time.Now().UTC()
	for index := 0; index < publicInteractionRequestCeiling; index++ {
		if !budget.Allow("source", now) {
			t.Fatalf("request %d must be allowed", index)
		}
	}
	if budget.Allow("source", now) {
		t.Fatal("the source ceiling must be enforced")
	}
	if !budget.Allow("source", now.Add(2*time.Minute)) {
		t.Fatal("a new window must reset the source counter")
	}
}

func TestPublicInteractionClientKeyIsBounded(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/public/interactions", nil)
	request.RemoteAddr = ""
	if expected, actual := "unknown", clientKey(request); expected != actual {
		t.Fatalf("clientKey() = %q, want %q", actual, expected)
	}
	request.RemoteAddr = "203.0.113.9:51234"
	if expected, actual := "203.0.113.9", clientKey(request); expected != actual {
		t.Fatalf("clientKey() = %q, want %q", actual, expected)
	}
}
