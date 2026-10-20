package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func helperAdminRequest(method, body string, pathValues map[string]string) *http.Request {
	request := httptest.NewRequest(method, "http://example.com/collaboration", strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "owner"}, Session: model.AdminSession{ID: "session"}}
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
}

func TestBodyErrorReasonContract(t *testing.T) {
	statuses := map[BodyErrorReason]int{
		BodyReasonOversize:  http.StatusRequestEntityTooLarge,
		BodyReasonMediaType: http.StatusUnsupportedMediaType,
		BodyReasonCancelled: http.StatusRequestTimeout,
		BodyReasonMalformed: http.StatusBadRequest,
		BodyReasonTrailing:  http.StatusBadRequest,
		BodyReasonInternal:  http.StatusBadRequest,
	}
	for reason, expected := range statuses {
		failure := &BodyError{Reason: reason}
		if failure.Status() != expected {
			t.Fatalf("status for %q = %d", reason, failure.Status())
		}
		if failure.Error() != "request body "+string(reason) {
			t.Fatalf("message for %q = %q", reason, failure.Message())
		}
	}
	messages := map[BodyErrorReason]string{
		BodyReasonOversize:  "request body too large",
		BodyReasonMediaType: "unsupported media type",
		BodyReasonCancelled: "request cancelled",
		BodyReasonTrailing:  "invalid request body",
	}
	for reason, expected := range messages {
		if value := (&BodyError{Reason: reason}).Message(); value != expected {
			t.Fatalf("message for %q = %q", reason, value)
		}
	}
	w := httptest.NewRecorder()
	WriteBodyError(w, errors.New("not a body error"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unclassified body error = %d", w.Code)
	}
	w = httptest.NewRecorder()
	WriteBodyError(w, &BodyError{Reason: BodyReasonOversize})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("classified body error = %d", w.Code)
	}
}

func TestReadAndDecodeBodyErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		classify func() error
		expected BodyErrorReason
	}{
		{"read cancelled", func() error { return readBodyError(t.Context(), context.Canceled) }, BodyReasonCancelled},
		{"read oversize", func() error { return readBodyError(t.Context(), &http.MaxBytesError{Limit: 8}) }, BodyReasonOversize},
		{"read internal", func() error { return readBodyError(t.Context(), errors.New("broken pipe")) }, BodyReasonInternal},
		{"decode cancelled", func() error { return decodeError(t.Context(), context.Canceled) }, BodyReasonCancelled},
		{"decode malformed", func() error { return decodeError(t.Context(), errors.New("bad json")) }, BodyReasonMalformed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var failure *BodyError
			if err := testCase.classify(); !errors.As(err, &failure) {
				t.Fatalf("classification = %v", err)
			} else if failure.Reason != testCase.expected {
				t.Fatalf("reason = %q, want %q", failure.Reason, testCase.expected)
			}
		})
	}
}

func TestDecodeJSONBodyRejections(t *testing.T) {
	oversized := strings.Repeat("a", 32)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oversized))
	if err := DecodeJSONBody(request, new(map[string]any), 8, false); err == nil {
		t.Fatal("oversized body was accepted")
	}
	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "text/plain")
	if err := DecodeJSONBody(request, new(map[string]any), 0, false); err == nil {
		t.Fatal("non JSON media type was accepted")
	}
	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{} {}"))
	request.Header.Set("Content-Type", "application/json")
	if err := DecodeJSONBody(request, new(map[string]any), 0, false); err == nil {
		t.Fatal("trailing JSON value was accepted")
	}
}

func TestRespondDeletionErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		failure  error
		expected int
	}{
		{"invalid request", model.ErrMetricErasureInvalid, http.StatusBadRequest},
		{"already running", model.ErrMetricErasureConflict, http.StatusConflict},
		{"missing target", model.ErrMetricErasureMissing, http.StatusNotFound},
		{"metadata forbidden", store.ErrMetadataForbidden, http.StatusForbidden},
		{"node busy", store.ErrOperationNodeBusy, http.StatusConflict},
		{"group busy", store.ErrOperationGroupBusy, http.StatusConflict},
		{"unknown failure", errors.New("storage down"), http.StatusInternalServerError},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			respondDeletionError(w, testCase.failure)
			if w.Code != testCase.expected {
				t.Fatalf("status = %d, want %d", w.Code, testCase.expected)
			}
		})
	}
}

func TestCreateDeletionJobRejections(t *testing.T) {
	w := httptest.NewRecorder()
	request := helperAdminRequest(http.MethodPost, "{}", map[string]string{"id": "node"})
	if _, _, ok := createDeletionJob(w, request, nil, model.MetricErasureDeleteNode, "node", time.Now()); ok {
		t.Fatal("deletion without store was accepted")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing store status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	request = helperAdminRequest(http.MethodPost, "{", map[string]string{"id": "node"})
	if _, _, ok := createDeletionJob(w, request, nil, model.MetricErasureDeleteNode, "node", time.Now()); ok {
		t.Fatal("malformed deletion body was accepted")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("malformed body status = %d", w.Code)
	}
}

type collaborationUnavailableCase struct {
	name   string
	method string
	body   string
	paths  map[string]string
	call   func(*DashboardCollaborationHandler) http.HandlerFunc
}

func collaborationUnavailableHandlers() []collaborationUnavailableCase {
	panelPaths := map[string]string{"id": "panel", "panelId": "panel"}
	return []collaborationUnavailableCase{
		{"list snapshots", http.MethodGet, "", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleListSnapshots }},
		{"create snapshot", http.MethodPost, "{}", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleCreateSnapshot }},
		{"revoke snapshot", http.MethodDelete, "", map[string]string{"id": "snapshot"}, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleRevokeSnapshot }},
		{"public snapshot", http.MethodGet, "", map[string]string{"token": strings.Repeat("a", 64)}, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandlePublicSnapshot }},
		{"list panel alerts", http.MethodGet, "", panelPaths, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleListPanelAlerts }},
		{"replace panel alerts", http.MethodPut, "{}", panelPaths, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleReplacePanelAlerts }},
		{"list library panels", http.MethodGet, "", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleListLibraryPanels }},
		{"create library panel", http.MethodPost, "{}", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleCreateLibraryPanel }},
		{"update library panel", http.MethodPut, "{}", map[string]string{"id": "panel"}, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleUpdateLibraryPanel }},
		{"delete library panel", http.MethodDelete, "", map[string]string{"id": "panel"}, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleDeleteLibraryPanel }},
		{"list annotations", http.MethodGet, "", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleListAnnotations }},
		{"public annotations", http.MethodGet, "", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandlePublicAnnotations }},
		{"create annotation", http.MethodPost, "{}", nil, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleCreateAnnotation }},
		{"update annotation", http.MethodPut, "{}", map[string]string{"id": "annotation"}, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleUpdateAnnotation }},
		{"delete annotation", http.MethodDelete, "", map[string]string{"id": "annotation"}, func(h *DashboardCollaborationHandler) http.HandlerFunc { return h.HandleDeleteAnnotation }},
	}
}

// TestCollaborationHandlersRejectMissingOrBrokenStores 覆盖协作面上依赖缺失与依赖损坏两条失败关闭路径。
func TestCollaborationHandlersRejectMissingOrBrokenStores(t *testing.T) {
	stores := closedHandlerStores(t)
	broken := NewDashboardCollaborationHandler(store.NewDashboardStore(stores.db),
		store.NewDashboardAnnotationStore(stores.db), store.NewDashboardSnapshotStore(stores.db),
		store.NewDashboardPanelLibraryStore(stores.db), store.NewDashboardPanelAlertStore(stores.db))
	missing := NewDashboardCollaborationHandler(nil, nil, nil, nil, nil)
	for _, testCase := range collaborationUnavailableHandlers() {
		t.Run(testCase.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testCase.call(missing)(w, helperAdminRequest(testCase.method, testCase.body, testCase.paths))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("missing dependency status = %d", w.Code)
			}
			w = httptest.NewRecorder()
			testCase.call(broken)(w, helperAdminRequest(testCase.method, testCase.body, testCase.paths))
			if w.Code < http.StatusBadRequest {
				t.Fatalf("broken dependency status = %d", w.Code)
			}
		})
	}
}
