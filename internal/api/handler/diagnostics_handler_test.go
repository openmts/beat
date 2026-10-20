package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/diagnostics"
	"github.com/beat/backend/internal/model"
)

// fakeDiagnostics 记录控制面收到的动作者与参数，并允许按用例注入错误。
type fakeDiagnostics struct {
	err          error
	actor        diagnostics.Actor
	filter       diagnostics.JobFilter
	query        diagnostics.ResultQuery
	change       diagnostics.TargetChange
	jobInput     diagnostics.JobInput
	proof        diagnostics.NodeProof
	document     diagnosticprotocol.CapabilityDocument
	attempt      diagnostics.AttemptRequest
	frame        diagnostics.ProgressFrame
	completion   diagnostics.Completion
	leaseRequest diagnostics.LeaseRequest
	cancel       bool
	sequence     int64
	jobID        string
	policy       model.DiagnosticPolicy
	target       model.DiagnosticTarget
	job          model.DiagnosticJob
	detail       diagnostics.JobDetail
	results      diagnostics.ResultSeries
	lease        *diagnostics.Lease
	digest       string
	calls        []string
}

func (fake *fakeDiagnostics) record(name string, actor diagnostics.Actor) {
	fake.calls = append(fake.calls, name)
	fake.actor = actor
}

func (fake *fakeDiagnostics) Policy(_ context.Context, actor diagnostics.Actor) (model.DiagnosticPolicy, error) {
	fake.record("policy", actor)
	return fake.policy, fake.err
}

func (fake *fakeDiagnostics) SavePolicy(_ context.Context, actor diagnostics.Actor, policy model.DiagnosticPolicy) (model.DiagnosticPolicy, error) {
	fake.record("save-policy", actor)
	fake.policy = policy
	return policy, fake.err
}

func (fake *fakeDiagnostics) ListTargets(_ context.Context, actor diagnostics.Actor) ([]model.DiagnosticTarget, error) {
	fake.record("list-targets", actor)
	return []model.DiagnosticTarget{fake.target}, fake.err
}

func (fake *fakeDiagnostics) GetTarget(_ context.Context, actor diagnostics.Actor, id string) (model.DiagnosticTarget, error) {
	fake.record("get-target", actor)
	fake.jobID = id
	return fake.target, fake.err
}

func (fake *fakeDiagnostics) SaveTarget(_ context.Context, actor diagnostics.Actor, change diagnostics.TargetChange) (model.DiagnosticTarget, error) {
	fake.record("save-target", actor)
	fake.change = change
	return fake.target, fake.err
}

func (fake *fakeDiagnostics) DeleteTarget(_ context.Context, actor diagnostics.Actor, id string) error {
	fake.record("delete-target", actor)
	fake.jobID = id
	return fake.err
}

func (fake *fakeDiagnostics) Capabilities(_ context.Context, actor diagnostics.Actor) ([]diagnostics.NodeCapability, error) {
	fake.record("capabilities", actor)
	return []diagnostics.NodeCapability{{NodeID: "node-1", Kinds: []string{model.DiagnosticKindMTR}}}, fake.err
}

func (fake *fakeDiagnostics) ListJobs(_ context.Context, actor diagnostics.Actor, filter diagnostics.JobFilter) ([]model.DiagnosticJob, error) {
	fake.record("list-jobs", actor)
	fake.filter = filter
	return []model.DiagnosticJob{fake.job}, fake.err
}

func (fake *fakeDiagnostics) Job(_ context.Context, actor diagnostics.Actor, id string) (diagnostics.JobDetail, error) {
	fake.record("job", actor)
	fake.jobID = id
	return fake.detail, fake.err
}

func (fake *fakeDiagnostics) CreateJob(_ context.Context, actor diagnostics.Actor, input diagnostics.JobInput) (model.DiagnosticJob, error) {
	fake.record("create-job", actor)
	fake.jobInput = input
	return fake.job, fake.err
}

func (fake *fakeDiagnostics) CancelJob(_ context.Context, actor diagnostics.Actor, id string) error {
	fake.record("cancel-job", actor)
	fake.jobID = id
	return fake.err
}

func (fake *fakeDiagnostics) RetryJob(_ context.Context, actor diagnostics.Actor, id string) (model.DiagnosticJob, error) {
	fake.record("retry-job", actor)
	fake.jobID = id
	return fake.job, fake.err
}

func (fake *fakeDiagnostics) DeleteJob(_ context.Context, actor diagnostics.Actor, id string) error {
	fake.record("delete-job", actor)
	fake.jobID = id
	return fake.err
}

func (fake *fakeDiagnostics) Results(_ context.Context, actor diagnostics.Actor, query diagnostics.ResultQuery) (diagnostics.ResultSeries, error) {
	fake.record("results", actor)
	fake.query = query
	return fake.results, fake.err
}

func (fake *fakeDiagnostics) Advertise(_ context.Context, proof diagnostics.NodeProof, document diagnosticprotocol.CapabilityDocument) (string, error) {
	fake.record("advertise", diagnostics.Actor{})
	fake.proof, fake.document = proof, document
	return fake.digest, fake.err
}

func (fake *fakeDiagnostics) Lease(_ context.Context, request diagnostics.LeaseRequest) (*diagnostics.Lease, error) {
	fake.record("lease", diagnostics.Actor{})
	fake.leaseRequest = request
	return fake.lease, fake.err
}

func (fake *fakeDiagnostics) Start(_ context.Context, attempt diagnostics.AttemptRequest) error {
	fake.record("start", diagnostics.Actor{})
	fake.attempt = attempt
	return fake.err
}

func (fake *fakeDiagnostics) Heartbeat(_ context.Context, attempt diagnostics.AttemptRequest) (bool, error) {
	fake.record("heartbeat", diagnostics.Actor{})
	fake.attempt = attempt
	return fake.cancel, fake.err
}

func (fake *fakeDiagnostics) Progress(_ context.Context, frame diagnostics.ProgressFrame) (int64, error) {
	fake.record("progress", diagnostics.Actor{})
	fake.frame = frame
	return fake.sequence, fake.err
}

func (fake *fakeDiagnostics) Complete(_ context.Context, completion diagnostics.Completion) error {
	fake.record("complete", diagnostics.Actor{})
	fake.completion = completion
	return fake.err
}

type diagnosticFixture struct {
	handler *DiagnosticHandler
	fake    *fakeDiagnostics
	actor   model.AdminPrincipal
}

func newDiagnosticFixture(t *testing.T) diagnosticFixture {
	t.Helper()
	base := newMetricDashboardFixture(t)
	fake := &fakeDiagnostics{digest: "digest-1", sequence: 7, jobID: "job-1",
		lease: &diagnostics.Lease{JobID: "job-1", Nonce: "nonce-1", Attempt: 1}}
	return diagnosticFixture{handler: NewDiagnosticHandler(fake), fake: fake, actor: base.principal}
}

type diagnosticCall struct {
	method    string
	query     string
	body      any
	id        string
	authorize string
	anonymous bool
	rawBody   string
}

func (fixture diagnosticFixture) call(t *testing.T, handle http.HandlerFunc, input diagnosticCall) *httptest.ResponseRecorder {
	t.Helper()
	payload := []byte(input.rawBody)
	if input.body != nil {
		encoded, err := json.Marshal(input.body)
		if err != nil {
			t.Fatal(err)
		}
		payload = encoded
	}
	method := input.method
	if method == "" {
		method = http.MethodPost
	}
	request := httptest.NewRequest(method, "http://example.com/diagnostics"+input.query, bytes.NewReader(payload))
	if input.id != "" {
		request.SetPathValue("id", input.id)
	}
	if input.authorize != "" {
		request.Header.Set("Authorization", input.authorize)
	}
	if !input.anonymous {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), fixture.actor))
	}
	response := httptest.NewRecorder()
	handle(response, request)
	return response
}

func expectDiagnosticStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("status = %d, want %d: %s", response.Code, expected, response.Body)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("diagnostic headers missing: %v", response.Header())
	}
}

func TestDiagnosticAdminHandlersForwardRequests(t *testing.T) {
	fixture := newDiagnosticFixture(t)
	fixture.fake.policy = model.DefaultDiagnosticPolicy()
	fixture.fake.target = model.DiagnosticTarget{ID: "target-1", Name: "edge"}
	fixture.fake.job = model.DiagnosticJob{ID: "job-1", State: model.DiagnosticStateQueued}
	targetInput := map[string]any{"name": "edge", "purpose": "path", "kinds": []string{"mtr"}, "address": "93.184.216.34",
		"protocol": "icmp", "direction": "forward", "ip_family": "auto", "maximum_bitrate_mbps": 100,
		"maximum_duration_seconds": 10, "maximum_parallel_streams": 1, "revision": 2}
	cases := []struct {
		name     string
		handle   http.HandlerFunc
		call     diagnosticCall
		expected int
		recorded string
	}{
		{"policy", fixture.handler.HandleDiagnosticPolicy, diagnosticCall{method: http.MethodGet}, http.StatusOK, "policy"},
		{"save policy", fixture.handler.HandleSaveDiagnosticPolicy, diagnosticCall{body: map[string]any{"enabled": true,
			"allowed_kinds": []string{"mtr"}, "maximum_active_jobs": 2, "maximum_active_jobs_per_node": 1,
			"maximum_iperf_duration_seconds": 10, "maximum_iperf_bitrate_mbps": 100, "revision": 1}}, http.StatusOK, "save-policy"},
		{"list targets", fixture.handler.HandleDiagnosticTargets, diagnosticCall{method: http.MethodGet}, http.StatusOK, "list-targets"},
		{"get target", fixture.handler.HandleDiagnosticTarget, diagnosticCall{method: http.MethodGet, id: "target-1"}, http.StatusOK, "get-target"},
		{"create target", fixture.handler.HandleCreateDiagnosticTarget, diagnosticCall{body: targetInput}, http.StatusOK, "save-target"},
		{"save target", fixture.handler.HandleSaveDiagnosticTarget, diagnosticCall{id: "target-1", body: targetInput}, http.StatusOK, "save-target"},
		{"delete target", fixture.handler.HandleDeleteDiagnosticTarget, diagnosticCall{method: http.MethodDelete, id: "target-1"}, http.StatusOK, "delete-target"},
		{"capabilities", fixture.handler.HandleDiagnosticCapabilities, diagnosticCall{method: http.MethodGet}, http.StatusOK, "capabilities"},
		{"list jobs", fixture.handler.HandleDiagnosticJobs, diagnosticCall{method: http.MethodGet,
			query: "?limit=5&state=queued&node_id=node-1&target_id=target-1"}, http.StatusOK, "list-jobs"},
		{"get job", fixture.handler.HandleDiagnosticJob, diagnosticCall{method: http.MethodGet, id: "job-1"}, http.StatusOK, "job"},
		{"create job", fixture.handler.HandleCreateDiagnosticJob, diagnosticCall{body: map[string]any{"source_node_id": "node-1",
			"target_id": "target-1", "kind": "mtr", "protocol": "icmp", "probes": 5, "hop_limit": 16,
			"confirmation": "edge"}}, http.StatusOK, "create-job"},
		{"cancel job", fixture.handler.HandleCancelDiagnosticJob, diagnosticCall{id: "job-1"}, http.StatusOK, "cancel-job"},
		{"retry job", fixture.handler.HandleRetryDiagnosticJob, diagnosticCall{id: "job-1"}, http.StatusOK, "retry-job"},
		{"delete job", fixture.handler.HandleDeleteDiagnosticJob, diagnosticCall{method: http.MethodDelete, id: "job-1"}, http.StatusOK, "delete-job"},
		{"results", fixture.handler.HandleDiagnosticResults, diagnosticCall{method: http.MethodGet, id: "job-1",
			query: "?metric=diagnostic_mtr_latency_ms&stat=avg&hop=3&limit=60"}, http.StatusOK, "results"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := fixture.call(t, testCase.handle, testCase.call)
			expectDiagnosticStatus(t, response, testCase.expected)
			if recorded := fixture.fake.calls[len(fixture.fake.calls)-1]; recorded != testCase.recorded {
				t.Fatalf("recorded = %q", recorded)
			}
			if fixture.fake.actor.UserID != fixture.actor.User.ID || fixture.fake.actor.Role != string(fixture.actor.User.Role) {
				t.Fatalf("actor = %+v", fixture.fake.actor)
			}
		})
	}
	if fixture.fake.change.ID != "target-1" || fixture.fake.change.ExpectedRevision != 2 {
		t.Fatalf("target change = %+v", fixture.fake.change)
	}
	if fixture.fake.filter.Limit != 5 || fixture.fake.filter.State != "queued" || fixture.fake.filter.SourceNode != "node-1" {
		t.Fatalf("job filter = %+v", fixture.fake.filter)
	}
	if fixture.fake.query.Hop != 3 || fixture.fake.query.Metric != "metric" && fixture.fake.query.Metric != "diagnostic_mtr_latency_ms" {
		t.Fatalf("result query = %+v", fixture.fake.query)
	}
}

func TestDiagnosticAdminHandlersRejectInvalidRequests(t *testing.T) {
	fixture := newDiagnosticFixture(t)
	cases := []struct {
		name     string
		handle   http.HandlerFunc
		call     diagnosticCall
		expected int
	}{
		{"unauthenticated", fixture.handler.HandleDiagnosticPolicy, diagnosticCall{method: http.MethodGet, anonymous: true}, http.StatusUnauthorized},
		{"unknown job filter key", fixture.handler.HandleDiagnosticJobs, diagnosticCall{method: http.MethodGet, query: "?severity=high"}, http.StatusBadRequest},
		{"job limit not a number", fixture.handler.HandleDiagnosticJobs, diagnosticCall{method: http.MethodGet, query: "?limit=many"}, http.StatusBadRequest},
		{"job body unknown field", fixture.handler.HandleCreateDiagnosticJob, diagnosticCall{body: map[string]any{"unknown": true}}, http.StatusBadRequest},
		{"job body malformed", fixture.handler.HandleCreateDiagnosticJob, diagnosticCall{rawBody: "{"}, http.StatusBadRequest},
		{"target body unknown field", fixture.handler.HandleCreateDiagnosticTarget, diagnosticCall{body: map[string]any{"name": "edge", "unknown": true}}, http.StatusBadRequest},
		{"policy body malformed", fixture.handler.HandleSaveDiagnosticPolicy, diagnosticCall{rawBody: "[]"}, http.StatusBadRequest},
		{"unknown result key", fixture.handler.HandleDiagnosticResults, diagnosticCall{method: http.MethodGet, id: "job-1", query: "?severity=warning"}, http.StatusBadRequest},
		{"hop not a number", fixture.handler.HandleDiagnosticResults, diagnosticCall{method: http.MethodGet, id: "job-1", query: "?hop=first"}, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := fixture.call(t, testCase.handle, testCase.call)
			expectDiagnosticStatus(t, response, testCase.expected)
		})
	}
}

func TestDiagnosticResponseErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected int
	}{
		{"invalid", diagnostics.ErrInvalid, http.StatusBadRequest},
		{"forbidden", diagnostics.ErrForbidden, http.StatusForbidden},
		{"capability", diagnostics.ErrCapability, http.StatusPreconditionFailed},
		{"conflict", diagnostics.ErrConflict, http.StatusConflict},
		{"not found", diagnostics.ErrNotFound, http.StatusNotFound},
		{"budget", diagnostics.ErrBudget, http.StatusTooManyRequests},
		{"unexpected", errors.New("boom"), http.StatusServiceUnavailable},
		{"wrapped invalid", errors.Join(errors.New("ctx"), diagnostics.ErrInvalid), http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newDiagnosticFixture(t)
			fixture.fake.err = testCase.err
			response := fixture.call(t, fixture.handler.HandleDiagnosticPolicy, diagnosticCall{method: http.MethodGet})
			expectDiagnosticStatus(t, response, testCase.expected)
		})
	}
	// 成功响应带同样的安全头并且是 200。
	fixture := newDiagnosticFixture(t)
	response := fixture.call(t, fixture.handler.HandleDiagnosticPolicy, diagnosticCall{method: http.MethodGet})
	expectDiagnosticStatus(t, response, http.StatusOK)
}

func TestDiagnosticAgentHandlers(t *testing.T) {
	fixture := newDiagnosticFixture(t)
	document := diagnosticprotocol.CapabilityDocument{Version: diagnosticprotocol.Version,
		Kinds: []string{model.DiagnosticKindMTR}, State: "ready",
		MTR: &diagnosticprotocol.MTRCapability{Binary: "/usr/bin/mtr", IPFamilies: []string{model.IPFamilyIPv4},
			Protocols: []string{model.DiagnosticProtocolICMP}},
		Maxima: diagnosticprotocol.CapabilityMaxima{Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops,
			DurationSeconds: model.DefaultIperfDurationSeconds, ParallelStreams: model.MinimumDiagnosticStreams,
			BitrateMbps: model.DefaultDiagnosticBitrateMbps}}
	advertise := fixture.call(t, fixture.handler.HandleDiagnosticAdvertise,
		diagnosticCall{body: document, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, advertise, http.StatusOK)
	if fixture.fake.proof.Token != "node-token" || len(fixture.fake.document.Kinds) != 1 {
		t.Fatalf("advertise proof = %+v / %+v", fixture.fake.proof, fixture.fake.document)
	}
	if !bytes.Contains(advertise.Body.Bytes(), []byte("digest-1")) {
		t.Fatalf("advertise body = %s", advertise.Body)
	}
	// 少了 Bearer 前缀时按无令牌上报，服务端负责拒绝。
	anonymous := fixture.call(t, fixture.handler.HandleDiagnosticAdvertise,
		diagnosticCall{body: document, authorize: "node-token", anonymous: true})
	expectDiagnosticStatus(t, anonymous, http.StatusOK)
	if fixture.fake.proof.Token != "" {
		t.Fatalf("unexpected token: %q", fixture.fake.proof.Token)
	}
	lease := fixture.call(t, fixture.handler.HandleDiagnosticLease, diagnosticCall{
		body: map[string]any{"capability_digest": "digest-1"}, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, lease, http.StatusOK)
	if fixture.fake.leaseRequest.CapabilityDigest != "digest-1" || fixture.fake.leaseRequest.Proof.Token != "node-token" {
		t.Fatalf("lease request = %+v", fixture.fake.leaseRequest)
	}
	attemptBody := map[string]any{"job_id": "job-1", "attempt": 1, "nonce": "nonce-1"}
	start := fixture.call(t, fixture.handler.HandleDiagnosticStart,
		diagnosticCall{id: "job-1", body: attemptBody, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, start, http.StatusOK)
	if fixture.fake.attempt.JobID != "job-1" || fixture.fake.attempt.Attempt != 1 || fixture.fake.attempt.Nonce != "nonce-1" {
		t.Fatalf("attempt = %+v", fixture.fake.attempt)
	}
	// 路径与正文的作业不一致必须拒绝，避免 Agent 用旧租约更新别的作业。
	mismatch := fixture.call(t, fixture.handler.HandleDiagnosticStart,
		diagnosticCall{id: "job-2", body: attemptBody, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, mismatch, http.StatusBadRequest)
	// 正文缺 job_id 时回落到路径值。
	pathed := fixture.call(t, fixture.handler.HandleDiagnosticStart, diagnosticCall{id: "job-1",
		body: map[string]any{"attempt": 1, "nonce": "nonce-1"}, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, pathed, http.StatusOK)
	if fixture.fake.attempt.JobID != "job-1" {
		t.Fatalf("pathed attempt = %+v", fixture.fake.attempt)
	}
	fixture.fake.cancel = true
	heartbeat := fixture.call(t, fixture.handler.HandleDiagnosticHeartbeat,
		diagnosticCall{id: "job-1", body: attemptBody, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, heartbeat, http.StatusOK)
	if !bytes.Contains(heartbeat.Body.Bytes(), []byte(`"cancel_requested":true`)) {
		t.Fatalf("heartbeat body = %s", heartbeat.Body)
	}
	progress := fixture.call(t, fixture.handler.HandleDiagnosticProgress, diagnosticCall{
		body: map[string]any{"job_id": "job-1", "nonce": "nonce-1", "attempt": 1, "sequence": 3,
			"samples": []map[string]any{{"metric": "diagnostic_mtr_latency_ms", "hop": 1, "stat": "avg", "value": 12.5}}},
		authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, progress, http.StatusOK)
	if fixture.fake.frame.Sequence != 3 || len(fixture.fake.frame.Samples) != 1 {
		t.Fatalf("progress frame = %+v", fixture.fake.frame)
	}
	complete := fixture.call(t, fixture.handler.HandleDiagnosticComplete, diagnosticCall{
		body:      map[string]any{"job_id": "job-1", "nonce": "nonce-1", "attempt": 1, "state": "succeeded"},
		authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, complete, http.StatusOK)
	if fixture.fake.completion.JobID != "job-1" || fixture.fake.completion.State != "succeeded" {
		t.Fatalf("completion = %+v", fixture.fake.completion)
	}
	// 畸形正文按无效请求处理。
	broken := fixture.call(t, fixture.handler.HandleDiagnosticAdvertise,
		diagnosticCall{rawBody: "{", authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, broken, http.StatusBadRequest)
}

func TestDiagnosticAgentCapacityIsBounded(t *testing.T) {
	fixture := newDiagnosticFixture(t)
	// 占满并发槽位后新的 Agent 请求按 429 拒绝，而不是无限排队。
	for index := 0; index < cap(fixture.handler.agentSlots); index += 1 {
		fixture.handler.agentSlots <- struct{}{}
	}
	response := fixture.call(t, fixture.handler.HandleDiagnosticLease, diagnosticCall{
		body: map[string]any{"capability_digest": "digest-1"}, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, response, http.StatusTooManyRequests)
	for index := 0; index < cap(fixture.handler.agentSlots); index += 1 {
		<-fixture.handler.agentSlots
	}
	// 槽位释放后同一请求恢复可用。
	released := fixture.call(t, fixture.handler.HandleDiagnosticLease, diagnosticCall{
		body: map[string]any{"capability_digest": "digest-1"}, authorize: "Bearer node-token", anonymous: true})
	expectDiagnosticStatus(t, released, http.StatusOK)
}
