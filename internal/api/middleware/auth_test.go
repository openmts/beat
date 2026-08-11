package middleware

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeAgentAuthenticator struct {
	tokenNode  *model.Node
	legacyNode *model.Node
	touched    string
}

func (f *fakeAgentAuthenticator) AuthenticateAgentToken(context.Context, string) (*model.Node, error) {
	return f.tokenNode, nil
}

func (f *fakeAgentAuthenticator) AuthenticateLegacyNode(context.Context, string) (*model.Node, error) {
	return f.legacyNode, nil
}

func (f *fakeAgentAuthenticator) TouchAgentToken(_ context.Context, nodeID string, _ time.Time, _ time.Duration) error {
	f.touched = nodeID
	return nil
}

func TestBearerAuth(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		header string
		want   int
	}{
		{name: "valid", token: "admin-secret", header: "Bearer admin-secret", want: http.StatusOK},
		{name: "scheme is case insensitive", token: "admin-secret", header: "bearer admin-secret", want: http.StatusOK},
		{name: "missing", token: "admin-secret", want: http.StatusUnauthorized},
		{name: "wrong", token: "admin-secret", header: "Bearer wrong", want: http.StatusUnauthorized},
		{name: "malformed", token: "admin-secret", header: "Basic value", want: http.StatusUnauthorized},
		{name: "not configured", header: "Bearer value", want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.header)
			w := httptest.NewRecorder()

			BearerAuth(tt.token)(okHandler()).ServeHTTP(w, req)

			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestWebSocketBearerAuth(t *testing.T) {
	validProtocol := WebSocketTokenProtocol("admin-secret")
	tests := []struct {
		name     string
		token    string
		protocol string
		want     int
	}{
		{name: "valid", token: "admin-secret", protocol: validProtocol, want: http.StatusOK},
		{name: "valid among protocols", token: "admin-secret", protocol: "terminal, " + validProtocol, want: http.StatusOK},
		{name: "missing", token: "admin-secret", want: http.StatusUnauthorized},
		{name: "invalid encoding", token: "admin-secret", protocol: webSocketTokenPrefix + "%%%", want: http.StatusUnauthorized},
		{name: "wrong", token: "admin-secret", protocol: WebSocketTokenProtocol("wrong"), want: http.StatusUnauthorized},
		{name: "not configured", protocol: validProtocol, want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Sec-WebSocket-Protocol", tt.protocol)
			w := httptest.NewRecorder()
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if got := SelectedWebSocketProtocol(r); got != tt.protocol && tt.name == "valid" {
					t.Errorf("selected protocol = %q, want %q", got, tt.protocol)
				}
				w.WriteHeader(http.StatusOK)
			})

			WebSocketBearerAuth(tt.token)(next).ServeHTTP(w, req)

			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if called != (tt.want == http.StatusOK) {
				t.Fatalf("called = %v, want %v", called, tt.want == http.StatusOK)
			}
		})
	}
}

func TestWebSocketTokenProtocol(t *testing.T) {
	token := "secret with spaces/and+symbols"
	protocol := WebSocketTokenProtocol(token)
	encoded := protocol[len(webSocketTokenPrefix):]
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode protocol: %v", err)
	}
	if string(decoded) != token {
		t.Fatalf("decoded token = %q, want %q", decoded, token)
	}
}

func TestAgentAuthBindsPerNodeIdentity(t *testing.T) {
	authenticator := &fakeAgentAuthenticator{tokenNode: &model.Node{ID: "node-a", Name: "alpha"}}
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("Authorization", "Bearer beat_agent_v1_"+
		"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE")
	response := httptest.NewRecorder()

	AgentAuth(authenticator, "legacy-secret", nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := AgentIdentity(r.Context())
		if !ok || identity.NodeID != "node-a" || identity.NodeName != "alpha" ||
			identity.Mode != model.AgentCredentialActive {
			t.Fatalf("identity = %#v, ok=%v", identity, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || authenticator.touched != "node-a" {
		t.Fatalf("status = %d, touched = %q", response.Code, authenticator.touched)
	}
}

func TestAgentAuthLegacyRequiresExistingLegacyNode(t *testing.T) {
	tests := []struct {
		name       string
		legacyNode *model.Node
		want       int
	}{
		{name: "existing legacy node", legacyNode: &model.Node{ID: "legacy", Name: "old"}, want: http.StatusOK},
		{name: "unknown node", want: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authenticator := &fakeAgentAuthenticator{legacyNode: test.legacyNode}
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request.Header.Set("Authorization", "Bearer legacy-secret")
			response := httptest.NewRecorder()
			resolver := func(*http.Request) (string, bool) { return "old", true }
			AgentAuth(authenticator, "legacy-secret", resolver)(okHandler()).ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestAgentAuthRejectsUnknownAndMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "unknown", "beat_agent_v1_invalid", "legacy-secret"} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		AgentAuth(&fakeAgentAuthenticator{}, "legacy-secret", nil)(okHandler()).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token %q status = %d, want 401", token, response.Code)
		}
	}
}

func TestWithAgentIdentity(t *testing.T) {
	want := model.AgentIdentity{NodeID: "node", NodeName: "name", Mode: model.AgentCredentialActive}
	got, ok := AgentIdentity(WithAgentIdentity(context.Background(), want))
	if !ok || got != want {
		t.Fatalf("identity = %#v, ok=%v", got, ok)
	}
}
