package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/remoteoperations"
)

// TestOperationResponseErrorMapping 覆盖远程操作响应把各领域错误映射为稳定状态码的分支。
func TestOperationResponseErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		failure  error
		expected int
	}{
		{"corrupt upload", remoteoperations.ErrCorrupt, http.StatusConflict},
		{"busy capacity", remoteoperations.ErrBusy, http.StatusServiceUnavailable},
		{"invalid request", remoteoperations.ErrInvalid, http.StatusBadRequest},
		{"forbidden actor", remoteoperations.ErrForbidden, http.StatusForbidden},
		{"state conflict", remoteoperations.ErrConflict, http.StatusConflict},
		{"missing entity", remoteoperations.ErrNotFound, http.StatusNotFound},
		{"unclassified failure", errors.New("storage down"), http.StatusServiceUnavailable},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			operationResponse(w, nil, testCase.failure)
			if w.Code != testCase.expected {
				t.Fatalf("status = %d, want %d", w.Code, testCase.expected)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("missing no-store on failure response")
			}
		})
	}
	w := httptest.NewRecorder()
	operationResponse(w, nil, remoteoperations.ErrBusy)
	if w.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy response retry-after = %q", w.Header().Get("Retry-After"))
	}
	w = httptest.NewRecorder()
	operationResponse(w, map[string]string{"id": "policy"}, nil)
	if w.Code != http.StatusOK || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("success response = %d, %q", w.Code, w.Header().Get("X-Content-Type-Options"))
	}
}

// TestRemoteOperationTaskQueryValidation 覆盖任务列表对非法分页参数与未知查询键的拒绝。
func TestRemoteOperationTaskQueryValidation(t *testing.T) {
	handler := NewRemoteOperationsHandler(remoteStub{})
	for _, raw := range []string{"limit=bad", "limit=-1&unexpected=1", "offset=1"} {
		w := httptest.NewRecorder()
		request := operationRequest("{}")
		request.URL.RawQuery = raw
		handler.HandleOperationTasks(w, request)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d", raw, w.Code)
		}
	}
	w := httptest.NewRecorder()
	request := operationRequest("{}")
	request.URL.RawQuery = "limit=10&cursor=next"
	handler.HandleOperationTasks(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("valid query status = %d", w.Code)
	}
}

// TestRemoteOperationSecondaryEndpoints 覆盖可执行策略、强制删除、任务内容与状态四个端点。
func TestRemoteOperationSecondaryEndpoints(t *testing.T) {
	handlers := func(handler *RemoteOperationsHandler) []http.HandlerFunc {
		return []http.HandlerFunc{handler.HandleExecutableOperationPolicies, handler.HandleForceDeleteOperationNode,
			handler.HandleOperationTaskContent, handler.HandleOperationStatus}
	}
	healthy := NewRemoteOperationsHandler(remoteStub{})
	for _, handle := range handlers(healthy) {
		w := httptest.NewRecorder()
		handle(w, operationRequest("{}"))
		if w.Code != http.StatusOK {
			t.Fatalf("healthy endpoint status = %d", w.Code)
		}
	}
	failing := NewRemoteOperationsHandler(remoteStub{err: remoteoperations.ErrNotFound})
	for _, handle := range handlers(failing) {
		w := httptest.NewRecorder()
		handle(w, operationRequest("{}"))
		if w.Code != http.StatusNotFound {
			t.Fatalf("failing endpoint status = %d", w.Code)
		}
	}
	failing = NewRemoteOperationsHandler(remoteStub{err: remoteoperations.ErrForbidden})
	for _, handle := range handlers(failing) {
		w := httptest.NewRecorder()
		handle(w, operationRequest("{}"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("forbidden endpoint status = %d", w.Code)
		}
	}
}

// TestRemoteOperationAgentCapacity 覆盖 agent 侧槽位打满时的 429 短路。
func TestRemoteOperationAgentCapacity(t *testing.T) {
	handler := NewRemoteOperationsHandler(remoteStub{digest: "digest"})
	for index := 0; index < cap(handler.agentSlots); index++ {
		handler.agentSlots <- struct{}{}
	}
	w := httptest.NewRecorder()
	handler.HandleOperationLease(w, operationRequest(`{"capability_digest":"digest","capabilities":{}}`))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("saturated agent slot status = %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("saturated response lost no-store")
	}
	for index := 0; index < cap(handler.agentSlots); index++ {
		handler.releaseAgent()
	}
	w = httptest.NewRecorder()
	handler.HandleOperationLease(w, operationRequest(`{"capability_digest":"digest","capabilities":{}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("released agent slot status = %d", w.Code)
	}
}
