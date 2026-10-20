package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type enrollmentStoreStub struct {
	issue   func(context.Context, store.EnrollmentTokenInput) (store.EnrollmentToken, error)
	consume func(context.Context, store.EnrollmentConsumeInput) (agentjoin.Result, error)
}

func (stub enrollmentStoreStub) CreateEnrollmentToken(ctx context.Context, input store.EnrollmentTokenInput) (store.EnrollmentToken, error) {
	return stub.issue(ctx, input)
}

func (stub enrollmentStoreStub) ConsumeEnrollmentToken(ctx context.Context, input store.EnrollmentConsumeInput) (agentjoin.Result, error) {
	return stub.consume(ctx, input)
}

func enrollmentHTTPRequest(body string, authenticated bool) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "https://beat.example/api/v1/agents/join", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{User: model.AdminUser{ID: "owner", Role: model.AdminRoleOwner}}))
	}
	return request
}

func TestAgentEnrollmentIssuesUniqueBoundedCredentials(t *testing.T) {
	var issued []store.EnrollmentTokenInput
	handler := NewAgentEnrollmentHandler(enrollmentStoreStub{issue: func(_ context.Context, input store.EnrollmentTokenInput) (store.EnrollmentToken, error) {
		issued = append(issued, input)
		return store.EnrollmentToken{ID: "enrollment-1", ServerURL: input.ServerURL, ExpiresAt: input.ExpiresAt}, nil
	}})
	var firstToken string
	for index := range 2 {
		response := httptest.NewRecorder()
		handler.HandleIssue(response, enrollmentHTTPRequest(`{"server_url":"https://BEAT.example/"}`, true))
		if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("issue status %d", response.Code)
		}
		var output agentjoin.Envelope[agentjoin.IssueResult]
		if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		if err := agentjoin.ValidateToken(output.Data.Token); err != nil {
			t.Fatal(err)
		}
		if output.Data.ServerURL != "https://beat.example" || !strings.Contains(output.Data.Command, "sudo ./beat-agent join") {
			t.Fatal("invalid command")
		}
		if index == 1 && firstToken == output.Data.Token {
			t.Fatal("reused token")
		}
		firstToken = output.Data.Token
	}
	if issued[0].ExpiresAt.Sub(issued[0].CreatedAt) != 30*time.Minute || issued[0].CreatorID != "owner" {
		t.Fatal("invalid issuance scope")
	}
}

func TestAgentEnrollmentIssueRejectsInvalidRequests(t *testing.T) {
	handler := NewAgentEnrollmentHandler(enrollmentStoreStub{})
	for _, test := range []struct {
		body      string
		principal bool
		media     string
		expected  int
	}{
		{`{"server_url":"https://beat.example"}`, false, "application/json", 401},
		{`{"server_url":"http://beat.example"}`, true, "application/json", 400},
		{`{"server_url":"https://beat.example/admin"}`, true, "application/json", 400},
		{`{"server_url":"https://beat.example","unknown":1}`, true, "application/json", 400},
		{`{"server_url":"https://beat.example"} {}`, true, "application/json", 400},
		{`{`, true, "application/json", 400},
		{`{}`, true, "text/plain", 415},
		{strings.Repeat(" ", agentjoin.MaximumBodyBytes+1), true, "application/json", 413},
	} {
		request := enrollmentHTTPRequest(test.body, test.principal)
		request.Header.Set("Content-Type", test.media)
		response := httptest.NewRecorder()
		handler.HandleIssue(response, request)
		if response.Code != test.expected {
			t.Fatalf("issue status=%d expected=%d", response.Code, test.expected)
		}
	}
}

func TestAgentEnrollmentIssueErrorsAndHTTPOptIn(t *testing.T) {
	for _, issueErr := range []error{nil, store.ErrEnrollmentCapacity, errors.New("database unavailable")} {
		handler := NewAgentEnrollmentHandler(enrollmentStoreStub{issue: func(_ context.Context, input store.EnrollmentTokenInput) (store.EnrollmentToken, error) {
			if !input.AllowHTTP {
				t.Error("HTTP opt-in was lost")
			}
			return store.EnrollmentToken{ID: "issued", ServerURL: input.ServerURL, ExpiresAt: input.ExpiresAt}, issueErr
		}})
		response := httptest.NewRecorder()
		handler.HandleIssue(response, enrollmentHTTPRequest(`{"server_url":"http://beat.example","allow_http":true}`, true))
		expected := 201
		if issueErr != nil {
			expected = 500
		}
		if errors.Is(issueErr, store.ErrEnrollmentCapacity) {
			expected = 429
		}
		if response.Code != expected {
			t.Fatalf("issue error status %d", response.Code)
		}
		if issueErr == nil && !strings.Contains(response.Body.String(), "--allow-http") {
			t.Fatal("missing HTTP command option")
		}
	}
	handler := NewAgentEnrollmentHandler(enrollmentStoreStub{})
	handler.generate = func() (agentjoin.Token, error) { return agentjoin.Token{}, errors.New("random unavailable") }
	response := httptest.NewRecorder()
	handler.HandleIssue(response, enrollmentHTTPRequest(`{"server_url":"https://beat.example"}`, true))
	if response.Code != 500 {
		t.Fatalf("random error status %d", response.Code)
	}
}

func TestAgentEnrollmentJoinValidatesIdentityAndMapsErrors(t *testing.T) {
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	payload := agentjoin.Request{RequestID: "request-1", AgentToken: credential.Plaintext, NodeName: "node-one", SSHPort: 22}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{nil, store.ErrEnrollmentUnauthorized, store.ErrEnrollmentIdentityUnavailable, store.ErrNodeNameConflict, store.ErrAgentTokenConflict, store.ErrEnrollmentSource, errors.New("database unavailable")} {
		handler := NewAgentEnrollmentHandler(enrollmentStoreStub{consume: func(_ context.Context, input store.EnrollmentConsumeInput) (agentjoin.Result, error) {
			if input.Request.RequestID != payload.RequestID || input.SourceIP != "192.0.2.1" || !input.Secure {
				t.Error("request binding lost")
			}
			return agentjoin.Result{NodeID: "node-1", Config: agentjoin.Config{AgentToken: credential.Plaintext}}, failure
		}})
		request := enrollmentHTTPRequest(string(body), false)
		request.Header.Set("Authorization", "Bearer "+token.Plaintext)
		response := httptest.NewRecorder()
		handler.HandleJoin(response, request)
		expected := 200
		switch failure {
		case nil:
		case store.ErrEnrollmentUnauthorized:
			expected = 401
		case store.ErrEnrollmentIdentityUnavailable:
			expected = 403
		case store.ErrNodeNameConflict, store.ErrAgentTokenConflict:
			expected = 409
		case store.ErrEnrollmentSource:
			expected = 400
		default:
			expected = 500
		}
		if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("join status %d expected %d", response.Code, expected)
		}
		if errors.Is(failure, store.ErrNodeNameConflict) && !strings.Contains(response.Body.String(), "--name") {
			t.Fatal("missing recovery instruction")
		}
		if failure != nil && strings.Contains(response.Body.String(), credential.Plaintext) {
			t.Fatal("error leaked credential")
		}
	}
}

func TestAgentEnrollmentJoinRejectsMalformedAuthAndBody(t *testing.T) {
	handler := NewAgentEnrollmentHandler(enrollmentStoreStub{})
	for _, authorization := range []string{"", "Basic invalid", "Bearer invalid", "Bearer a b"} {
		request := enrollmentHTTPRequest(`{}`, false)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.HandleJoin(response, request)
		if response.Code != 401 {
			t.Fatalf("auth status %d", response.Code)
		}
	}
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `{"request_id":"bad id"}`, `{"unknown":true}`} {
		request := enrollmentHTTPRequest(body, false)
		request.Header.Set("Authorization", "Bearer "+token.Plaintext)
		response := httptest.NewRecorder()
		handler.HandleJoin(response, request)
		if response.Code != 400 {
			t.Fatalf("body status %d", response.Code)
		}
	}
}
