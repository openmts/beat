package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type reportReadOnlyAuthenticator struct{ touches int }

func (*reportReadOnlyAuthenticator) AuthenticateAgentToken(context.Context, string) (*model.Node, error) {
	return &model.Node{ID: "node", Name: "name", AgentTokenHash: make([]byte, 32)}, nil
}

func (*reportReadOnlyAuthenticator) AuthenticateLegacyNode(context.Context, string) (*model.Node, error) {
	return nil, nil
}

func (auth *reportReadOnlyAuthenticator) TouchAgentToken(context.Context, string, time.Time, time.Duration) error {
	auth.touches++
	return nil
}

func TestReportAuthenticationOnlyReadsUntilIngressAdmission(t *testing.T) {
	auth := &reportReadOnlyAuthenticator{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := AgentIdentity(r.Context())
		if !ok || identity.CredentialFingerprint == "" {
			t.Error("read-only authentication lost credential binding")
		}
		w.WriteHeader(429)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", nil)
	request.Header.Set("Authorization", "Bearer example")
	response := httptest.NewRecorder()
	AgentReportAuth(auth, "", nil)(next).ServeHTTP(response, request)
	if response.Code != 429 || auth.touches != 0 {
		t.Fatal("rejected report touched credential usage")
	}
}
