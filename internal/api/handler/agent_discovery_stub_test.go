package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// discoveryStubNow 是协议覆盖率夹具统一使用的固定时钟。
var discoveryStubNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// discoveryStubStore 只实现协议路径需要的存储方法；未实现的方法保持接口零值以便暴露越界调用。
type discoveryStubStore struct {
	AgentDiscoveryOperations
	grant        *store.EnrollmentGrant
	grantErr     error
	challenge    store.EnrollmentChallenge
	challengeErr error
	consumed     store.EnrollmentChallenge
	consumeErr   error
	created      store.EnrollmentRequest
	resumed      bool
	createErr    error
	request      *store.EnrollmentRequest
	requestErr   error
	rotated      store.EnrollmentRequest
	rotateErr    error
	claimResult  store.EnrollmentClaimResult
	claimErr     error
}

func (stub discoveryStubStore) GrantBySecret(context.Context, string, time.Time) (*store.EnrollmentGrant, error) {
	return stub.grant, stub.grantErr
}

func (stub discoveryStubStore) CreateChallenge(context.Context,
	store.EnrollmentChallengeInput) (store.EnrollmentChallenge, error) {
	return stub.challenge, stub.challengeErr
}

func (stub discoveryStubStore) ConsumeChallenge(context.Context,
	store.EnrollmentChallengeProof) (store.EnrollmentChallenge, error) {
	return stub.consumed, stub.consumeErr
}

func (stub discoveryStubStore) CreateOrResumeRequest(context.Context,
	store.EnrollmentRequestInput) (store.EnrollmentRequest, bool, error) {
	return stub.created, stub.resumed, stub.createErr
}

func (stub discoveryStubStore) EnrollmentRequest(context.Context, string) (*store.EnrollmentRequest, error) {
	return stub.request, stub.requestErr
}

func (stub discoveryStubStore) RotateEnrollmentTicket(context.Context, string,
	agentenrollment.Secret, time.Time) (store.EnrollmentRequest, error) {
	return stub.rotated, stub.rotateErr
}

func (stub discoveryStubStore) ClaimEnrollmentRequest(context.Context, store.EnrollmentClaim,
	agentcredential.Token, time.Time) (store.EnrollmentClaimResult, error) {
	return stub.claimResult, stub.claimErr
}

// discoveryStubGrant 返回一条来源、主机名都放行的授权。
func discoveryStubGrant() *store.EnrollmentGrant {
	return &store.EnrollmentGrant{ID: "grant-1", Name: "lab", Mode: agentenrollment.ApprovalManual,
		MaximumEnrollments: 5, SSHPort: 22, DefaultGroupID: "group-1"}
}

// discoveryStubHandler 用桩存储构造协议处理器，并固定时钟。
func discoveryStubHandler(stub discoveryStubStore) *AgentDiscoveryHandler {
	subject := NewAgentDiscoveryHandler(stub, func(*http.Request) string { return discoveryTestOrigin }, false)
	subject.now = func() time.Time { return discoveryStubNow }
	return subject
}

// discoveryPostOptions 描述一次协议请求的原始要素。
type discoveryPostOptions struct {
	handler     http.HandlerFunc
	path        string
	token       string
	contentType string
	body        string
	pathID      string
	remoteAddr  string
}

// postDiscoveryRaw 发送一次可指定媒体类型与来源地址的协议请求。
func postDiscoveryRaw(t *testing.T, options discoveryPostOptions) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, options.path, strings.NewReader(options.body))
	if options.contentType != "" {
		request.Header.Set("Content-Type", options.contentType)
	}
	if options.token != "" {
		request.Header.Set("Authorization", "Bearer "+options.token)
	}
	if options.pathID != "" {
		request.SetPathValue("id", options.pathID)
	}
	if options.remoteAddr != "" {
		request.RemoteAddr = options.remoteAddr
	}
	recorder := httptest.NewRecorder()
	options.handler(recorder, request)
	return recorder
}

// discoveryGrantToken 生成一枚格式合法但未必存在的授权秘密。
func discoveryGrantToken(t *testing.T) string {
	t.Helper()
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate grant secret: %v", err)
	}
	return secret.Plaintext
}

// discoveryRequestToken 生成一枚格式合法的请求令牌。
func discoveryRequestToken(t *testing.T) agentenrollment.Secret {
	t.Helper()
	secret, err := agentenrollment.GenerateRequestToken()
	if err != nil {
		t.Fatalf("generate request token: %v", err)
	}
	return secret
}

// encodeDiscoveryCaseBody 序列化表驱动用例的请求体，raw 优先。
func encodeDiscoveryCaseBody(t *testing.T, raw string, value any) string {
	t.Helper()
	if raw != "" {
		return raw
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return string(encoded)
}

// assertDiscoveryStatus 断言状态码并保留响应体用于诊断。
func assertDiscoveryStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()
	if recorder.Code != want {
		t.Fatalf("status = %d want %d body = %s", recorder.Code, want, recorder.Body.String())
	}
}
