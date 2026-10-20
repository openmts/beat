package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type unavailableAgentAuthenticator struct {
	node                           *model.Node
	lookupErr, touchErr, legacyErr error
}

func (auth unavailableAgentAuthenticator) AuthenticateAgentToken(context.Context, string) (*model.Node, error) {
	return auth.node, auth.lookupErr
}

func (auth unavailableAgentAuthenticator) AuthenticateLegacyNode(context.Context, string) (*model.Node, error) {
	return nil, auth.legacyErr
}

func (auth unavailableAgentAuthenticator) TouchAgentToken(context.Context, string, time.Time, time.Duration) error {
	return auth.touchErr
}

func TestAgentAuthenticationStorageFailuresPreventReporting(t *testing.T) {
	unavailable := errors.New("private database failure")
	for _, test := range []struct {
		name         string
		auth         unavailableAgentAuthenticator
		resolvedName string
		expected     int
	}{
		{"lookup unavailable", unavailableAgentAuthenticator{lookupErr: unavailable}, "node", 500},
		{"credential touch unavailable", unavailableAgentAuthenticator{node: &model.Node{ID: "node", Name: "node"}, touchErr: unavailable}, "node", 500},
		{"legacy lookup unavailable", unavailableAgentAuthenticator{legacyErr: unavailable}, "node", 500},
		{"legacy identity missing", unavailableAgentAuthenticator{}, "", 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			resolver := func(*http.Request) (string, bool) { return test.resolvedName, true }
			request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", nil)
			request.Header.Set("Authorization", "Bearer legacy-secret")
			response := httptest.NewRecorder()
			AgentAuth(test.auth, "legacy-secret", resolver)(next).ServeHTTP(response, request)
			if response.Code != test.expected || called {
				t.Fatalf("status=%d reporting reached=%t", response.Code, called)
			}
			if strings.Contains(response.Body.String(), unavailable.Error()) {
				t.Fatal("authentication response disclosed storage details")
			}
		})
	}
}
