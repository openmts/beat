package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/remoteoperations"
)

type remoteStub struct {
	RemoteOperations
	err    error
	digest string
}

func (stub remoteStub) SavePolicy(context.Context, remoteoperations.Actor, remoteoperations.PolicyInput) (string, error) {
	return "policy", stub.err
}

func (stub remoteStub) ListPolicies(context.Context, remoteoperations.Actor) ([]remoteoperations.PolicyInput, error) {
	return []remoteoperations.PolicyInput{}, stub.err
}

func (stub remoteStub) SaveSnippet(context.Context, remoteoperations.Actor, remoteoperations.SnippetInput) (remoteoperations.SnippetVersion, error) {
	return remoteoperations.SnippetVersion{ID: "snippet"}, stub.err
}

func (stub remoteStub) ListSnippets(context.Context, remoteoperations.Actor) ([]remoteoperations.SnippetVersion, error) {
	return []remoteoperations.SnippetVersion{}, stub.err
}

func (stub remoteStub) SnippetContent(context.Context, remoteoperations.Actor, remoteoperations.SnippetVersion) ([]byte, error) {
	return []byte("<script>"), stub.err
}

func (stub remoteStub) ApproveSnippet(context.Context, remoteoperations.Actor, remoteoperations.SnippetVersion) error {
	return stub.err
}

func (stub remoteStub) CreateTask(context.Context, remoteoperations.Actor, remoteoperations.TaskInput) (string, error) {
	return "task", stub.err
}

func (stub remoteStub) ListTasks(context.Context, remoteoperations.Actor) ([]remoteoperations.TaskSummary, error) {
	return []remoteoperations.TaskSummary{}, stub.err
}

func (stub remoteStub) GetTask(context.Context, remoteoperations.Actor, string) (remoteoperations.TaskSummary, error) {
	return remoteoperations.TaskSummary{ID: "task"}, stub.err
}

func (stub remoteStub) CancelTask(context.Context, remoteoperations.Actor, string) error {
	return stub.err
}

func (stub remoteStub) Advertise(context.Context, remoteoperations.NodeProof, remoteoperations.Policy) (string, error) {
	return stub.digest, stub.err
}

func (stub remoteStub) Lease(context.Context, remoteoperations.LeaseRequest) (*remoteoperations.Lease, error) {
	return nil, stub.err
}

func (stub remoteStub) Start(context.Context, remoteoperations.AttemptRequest) error { return stub.err }

func (stub remoteStub) Heartbeat(context.Context, remoteoperations.AttemptRequest) (bool, error) {
	return true, stub.err
}

func (stub remoteStub) Complete(context.Context, remoteoperations.Completion) error { return stub.err }

func (stub remoteStub) RetryTarget(context.Context, remoteoperations.Actor, remoteoperations.RetryInput) error {
	return stub.err
}

func (stub remoteStub) TargetOutput(context.Context, remoteoperations.Actor, remoteoperations.TargetRequest) (remoteoperations.TargetOutput, error) {
	return remoteoperations.TargetOutput{Result: remoteoperations.Result{State: "succeeded"}}, stub.err
}

func (stub remoteStub) GetPolicy(context.Context, remoteoperations.Actor, string) (remoteoperations.PolicyInput, error) {
	return remoteoperations.PolicyInput{ID: "policy"}, stub.err
}

func (stub remoteStub) DisablePolicy(context.Context, remoteoperations.Actor, string) error {
	return stub.err
}

func (stub remoteStub) DeleteSnippet(context.Context, remoteoperations.Actor, string) error {
	return stub.err
}

func (stub remoteStub) ListTaskPage(context.Context, remoteoperations.Actor, remoteoperations.TaskPageQuery) (remoteoperations.TaskPage, error) {
	return remoteoperations.TaskPage{Items: []remoteoperations.TaskSummary{}}, stub.err
}

func (stub remoteStub) ExecutablePolicies(context.Context, remoteoperations.Actor) ([]remoteoperations.PolicyInput, error) {
	return []remoteoperations.PolicyInput{}, stub.err
}

func (stub remoteStub) Status(context.Context, remoteoperations.Actor) (remoteoperations.Status, error) {
	return remoteoperations.Status{}, stub.err
}

func (stub remoteStub) ForceDeleteNode(context.Context, remoteoperations.Actor, remoteoperations.ForceNodeDelete) error {
	return stub.err
}

func (stub remoteStub) TaskContent(context.Context, remoteoperations.Actor, string) (remoteoperations.TaskContent, error) {
	return remoteoperations.TaskContent{ID: "task"}, stub.err
}

func operationRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/operations", strings.NewReader(body))
	r = r.WithContext(middleware.WithAdminPrincipal(r.Context(), model.AdminPrincipal{User: model.AdminUser{ID: "owner"}, Session: model.AdminSession{ID: "session"}}))
	r.SetPathValue("id", "resource")
	r.Header.Set("Authorization", "Bearer fixture")
	return r
}

func TestRemoteOperationAdminHandlersAndRedaction(t *testing.T) {
	h := NewRemoteOperationsHandler(remoteStub{})
	for _, handle := range []http.HandlerFunc{h.HandleOperationPolicy, h.HandleDisableOperationPolicy, h.HandleDeleteOperationSnippet, h.HandleOperationPolicies, h.HandleSaveOperationPolicy, h.HandleOperationTasks, h.HandleOperationTask, h.HandleCreateOperationTask,
		h.HandleCancelOperationTask, h.HandleRetryOperationTarget, h.HandleOperationTargetOutput, h.HandleOperationSnippets, h.HandleSaveOperationSnippet, h.HandleApproveOperationSnippet} {
		w := httptest.NewRecorder()
		handle(w, operationRequest("{}"))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "<script>") {
			t.Fatal(w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		handle(w, httptest.NewRequest(http.MethodPost, "/", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatal("missing principal admitted")
		}
	}
	for _, handle := range []http.HandlerFunc{h.HandleSaveOperationPolicy, h.HandleCreateOperationTask, h.HandleSaveOperationSnippet, h.HandleApproveOperationSnippet, h.HandleRetryOperationTarget} {
		w := httptest.NewRecorder()
		handle(w, operationRequest("{\"unknown\":1}"))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	r := operationRequest("{}")
	r.URL.RawQuery = "version=bad"
	w := httptest.NewRecorder()
	h.HandleOperationSnippetContent(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	h = NewRemoteOperationsHandler(remoteStub{err: remoteoperations.ErrForbidden})
	w = httptest.NewRecorder()
	r = operationRequest("{}")
	r.URL.RawQuery = "version=1"
	h.HandleOperationSnippetContent(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestRemoteOperationAgentHandlers(t *testing.T) {
	h := NewRemoteOperationsHandler(remoteStub{digest: "digest"})
	for _, item := range []struct {
		handle http.HandlerFunc
		body   string
	}{
		{h.HandleOperationLease, `{"capability_digest":"digest","capabilities":{}}`},
		{h.HandleOperationStart, `{"nonce":"nonce"}`}, {h.HandleOperationHeartbeat, `{"nonce":"nonce"}`},
		{h.HandleOperationComplete, `{"nonce":"nonce","result":{"state":"succeeded"}}`},
	} {
		w := httptest.NewRecorder()
		item.handle(w, operationRequest(item.body))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		item.handle(w, operationRequest("bad"))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.HandleOperationLease(w, operationRequest(`{"capability_digest":"stale","capabilities":{}}`))
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	h = NewRemoteOperationsHandler(remoteStub{err: remoteoperations.ErrForbidden})
	w = httptest.NewRecorder()
	h.HandleOperationLease(w, operationRequest(`{}`))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if operationNode(httptest.NewRequest(http.MethodGet, "/", nil)).Token != "" {
		t.Fatal("missing bearer")
	}
}

func TestRemoteOperationErrorsNeverReflectContent(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
	}{{remoteoperations.ErrInvalid, 400}, {remoteoperations.ErrForbidden, 403}, {remoteoperations.ErrConflict, 409}, {remoteoperations.ErrNotFound, 404}, {remoteoperations.ErrUnavailable, 503}} {
		w := httptest.NewRecorder()
		operationResponse(w, nil, item.err)
		if w.Code != item.status {
			t.Fatal(w.Code)
		}
		if !strings.Contains(w.Body.String(), "operation request was rejected") {
			t.Fatal(w.Body.String())
		}
	}
}

func TestRemoteOperationDecoderBoundsGzipAndUnknownFields(t *testing.T) {
	for _, body := range []string{"", "{} {}", `{"unknown":true}`, `{"Nonce":"` + strings.Repeat("x", maximumOperationBodyBytes) + `"}`} {
		var target remoteoperations.AgentAttemptInput
		if decodeOperation(operationRequest(body), &target) != remoteoperations.ErrInvalid {
			t.Fatal("invalid body admitted")
		}
	}
	for _, encoding := range []string{"br", "gzip"} {
		r := operationRequest("bad")
		r.Header.Set("Content-Encoding", encoding)
		if decodeOperation(r, &remoteoperations.AgentAttemptInput{}) != remoteoperations.ErrInvalid {
			t.Fatal("invalid compression")
		}
	}
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write([]byte(`{"nonce":"valid"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := operationRequest(encoded.String())
	r.Header.Set("Content-Encoding", "gzip")
	var target remoteoperations.AgentAttemptInput
	if err := decodeOperation(r, &target); err != nil || target.Nonce != "valid" {
		t.Fatal(err)
	}
	r.Body = nil
	if decodeOperation(r, &target) != remoteoperations.ErrInvalid {
		t.Fatal("nil body")
	}
}
