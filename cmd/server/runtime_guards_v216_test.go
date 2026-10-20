package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/deploymentaccess"
)

func TestMetricOptionsTolerateMissingSQLite(t *testing.T) {
	for name, stores := range map[string]*serverStores{"nil": nil, "empty": {}} {
		if option := metricCatalogOption(stores); option == nil {
			t.Fatalf("%s stores produced a nil metric catalog option", name)
		}
		if runner := serverMetricRetention(stores); runner != nil {
			t.Fatalf("%s stores produced a retention runner", name)
		}
		if option := metricDashboardOption(stores); option == nil {
			t.Fatalf("%s stores produced a nil dashboard option", name)
		}
	}
}

func TestDeploymentAccessOrDefaultPreservesInjectedPolicy(t *testing.T) {
	configured, err := deploymentaccess.Load(func(key string) string {
		if key == deploymentaccess.EnvPublicAPIOrigin {
			return "https://console.example.com"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("load configured access policy: %v", err)
	}
	if resolved := deploymentAccessOrDefault(configured); resolved.APIOrigins().Mode() != configured.APIOrigins().Mode() {
		t.Fatalf("injected policy was replaced: %s", resolved.APIOrigins().Mode())
	}
	fallback := deploymentAccessOrDefault(deploymentaccess.Config{})
	if fallback.APIOrigins().Mode() == deploymentaccess.OriginModeSameOrigin {
		t.Fatal("same-origin placeholder kept its placeholder policy")
	}
}

func TestStaticCacheControlFallsBackToNoCache(t *testing.T) {
	recorder := httptest.NewRecorder()
	setStaticCacheControl(recorder, "index.html")
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("html Cache-Control = %q", got)
	}
	asset := httptest.NewRecorder()
	setStaticCacheControl(asset, "assets/app.js")
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q", got)
	}
}

func TestIngressIdentityAndLimiterGuards(t *testing.T) {
	session := httptest.NewRequest(http.MethodGet, "/admin", nil)
	session.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: "token"})
	if identity := ingressIdentity(session); len(identity) <= len("session:") {
		t.Fatalf("session identity = %q", identity)
	}
	anonymous := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if identity := ingressIdentity(anonymous); len(identity) <= len("ip:") {
		t.Fatalf("anonymous identity = %q", identity)
	}
	emptyCookie := httptest.NewRequest(http.MethodGet, "/admin", nil)
	emptyCookie.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: ""})
	if identity := ingressIdentity(emptyCookie); identity[:3] != "ip:" {
		t.Fatalf("empty cookie identity = %q", identity)
	}
	if limiter := ingressLimiter(nil); limiter != nil {
		t.Fatal("nil runtime produced an ingress limiter")
	}
	state := newRuntimeState()
	if limiter := ingressLimiter(state); limiter != nil {
		t.Fatal("unconfigured runtime produced an ingress limiter")
	}
	if snapshot := state.ingressSnapshot(); snapshot != nil {
		t.Fatal("unconfigured runtime produced an ingress snapshot")
	}
	limiter, err := initializeIngress(func(string) string { return "" })
	if err != nil {
		t.Fatalf("default ingress configuration: %v", err)
	}
	state.ingress.Store(limiter)
	if snapshot := state.ingressSnapshot(); snapshot == nil {
		t.Fatal("configured runtime lost its ingress snapshot")
	}
}

func TestInitializeIngressRejectsInvalidCapacity(t *testing.T) {
	if _, err := initializeIngress(func(string) string { return "not-a-number" }); err == nil {
		t.Fatal("invalid ingress capacity was accepted")
	}
}
