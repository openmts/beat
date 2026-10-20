package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/ingress"
)

type stubLimiter struct {
	class     ingress.Class
	identity  string
	calls     int
	rejection *ingress.Rejection
}

func (limiter *stubLimiter) Admit(class ingress.Class, identity string) (ingress.Lease, *ingress.Rejection) {
	limiter.calls++
	limiter.class = class
	limiter.identity = identity
	if limiter.rejection != nil {
		return ingress.Lease{}, limiter.rejection
	}
	return ingress.Lease{}, nil
}

func TestAdmissionRejectsBeforeHandlerWithBoundedResponse(t *testing.T) {
	limiter := &stubLimiter{rejection: &ingress.Rejection{
		Reason: ingress.ReasonClassRate, Status: http.StatusTooManyRequests, RetryAfter: 7,
	}}
	handled := false
	handler := Admission(limiter, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handled = true
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))

	if handled {
		t.Fatal("rejected request must not reach the handler")
	}
	if expected, actual := http.StatusTooManyRequests, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if expected, actual := "7", recorder.Header().Get("Retry-After"); expected != actual {
		t.Fatalf("expected retry-after %s, actual %s", expected, actual)
	}
	if expected, actual := "nosniff", recorder.Header().Get("X-Content-Type-Options"); expected != actual {
		t.Fatalf("expected nosniff, actual %s", actual)
	}
	if expected, actual := `{"code":429,"message":"request rejected"}`, recorder.Body.String(); expected != actual {
		t.Fatalf("expected generic message, actual %s", actual)
	}
	if expected, actual := ingress.PublicSnapshot, limiter.class; expected != actual {
		t.Fatalf("expected class %s, actual %s", expected, actual)
	}
}

func TestAdmissionSkipsUndeclaredPaths(t *testing.T) {
	limiter := &stubLimiter{rejection: &ingress.Rejection{Reason: ingress.ReasonClassRate, Status: 429, RetryAfter: 1}}
	handler := Admission(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/index.js", nil))

	if expected, actual := 0, limiter.calls; expected != actual {
		t.Fatalf("expected no admission call, actual %d", actual)
	}
	if expected, actual := http.StatusNoContent, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
}

func TestAdmissionUsesProvidedIdentityAndOverride(t *testing.T) {
	limiter := &stubLimiter{}
	identity := func(*http.Request) string { return "session:abc" }
	handler := Admission(limiter, identity)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	if expected, actual := "session:abc", limiter.identity; expected != actual {
		t.Fatalf("expected identity %s, actual %s", expected, actual)
	}

	blank := &stubLimiter{}
	handler = Admission(blank, func(*http.Request) string { return "" })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	request.RemoteAddr = "203.0.113.9:5000"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if expected, actual := "ip:203.0.113.9", blank.identity; expected != actual {
		t.Fatalf("expected identity %s, actual %s", expected, actual)
	}
}

func TestAdmissionIdentityPrefersResolver(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	request.RemoteAddr = "203.0.113.9:5000"
	if expected, actual := "custom", AdmissionIdentity(func(*http.Request) string { return "custom" }, request); expected != actual {
		t.Fatalf("expected %s, actual %s", expected, actual)
	}
	if expected, actual := "ip:203.0.113.9", AdmissionIdentity(nil, request); expected != actual {
		t.Fatalf("expected %s, actual %s", expected, actual)
	}
}

func TestFingerprintHidesRawSessionTokens(t *testing.T) {
	first := Fingerprint("secret-session-token")
	second := Fingerprint("secret-session-token")
	if first != second {
		t.Fatal("fingerprint must be deterministic")
	}
	if first == "secret-session-token" || len(first) != 32 {
		t.Fatalf("fingerprint must be a fixed-size digest, actual %q", first)
	}
	if Fingerprint("other") == first {
		t.Fatal("different tokens must not collide")
	}
}

func TestAdmissionDisabledWithoutLimiter(t *testing.T) {
	handler := Admission(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	if expected, actual := http.StatusTeapot, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
}
