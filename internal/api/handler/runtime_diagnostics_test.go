package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/runtimediag"
)

type fakeRuntimeDiagnostics struct {
	captures    []runtimediag.Capture
	startErr    error
	downloadErr error
	cancelErr   error
	content     string
	started     []string
	revoked     []string
	cancelled   []string
}

func (fake *fakeRuntimeDiagnostics) Summary() runtimediag.Summary {
	return runtimediag.Summary{Version: "0.2.0", GoVersion: "go1.25"}
}

func (fake *fakeRuntimeDiagnostics) List(string) []runtimediag.Capture { return fake.captures }

func (fake *fakeRuntimeDiagnostics) Busy() bool { return len(fake.captures) > 0 }

func (fake *fakeRuntimeDiagnostics) Start(_ context.Context, _, _, kind string, _ time.Duration) (runtimediag.Capture, error) {
	fake.started = append(fake.started, kind)
	if fake.startErr != nil {
		return runtimediag.Capture{}, fake.startErr
	}
	return runtimediag.Capture{ID: "0123456789abcdef", Kind: kind, Status: runtimediag.StatusRunning}, nil
}

func (fake *fakeRuntimeDiagnostics) Download(_, _ string) (io.ReadCloser, runtimediag.Capture, error) {
	if fake.downloadErr != nil {
		return nil, runtimediag.Capture{}, fake.downloadErr
	}
	return io.NopCloser(strings.NewReader(fake.content)),
		runtimediag.Capture{ID: "0123456789abcdef", Kind: runtimediag.KindCPU, Bytes: int64(len(fake.content))}, nil
}

func (fake *fakeRuntimeDiagnostics) Cancel(_, id string) error {
	if fake.cancelErr != nil {
		return fake.cancelErr
	}
	fake.cancelled = append(fake.cancelled, id)
	return nil
}

func (fake *fakeRuntimeDiagnostics) RevokeSession(sessionID string) {
	fake.revoked = append(fake.revoked, sessionID)
}

func ownerRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "owner-1", Role: model.AdminRoleOwner}, Session: model.AdminSession{ID: "session-1"}}
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
}

func TestRuntimeDiagnosticsSummaryAndList(t *testing.T) {
	fake := &fakeRuntimeDiagnostics{captures: []runtimediag.Capture{{ID: "a", Kind: runtimediag.KindHeap}}}
	audits := map[string]string{}
	handler := NewRuntimeDiagnosticsHandler(fake, func(_ *http.Request, action, id string) { audits[action] = id })

	recorder := httptest.NewRecorder()
	handler.HandleSummary(recorder, ownerRequest(http.MethodGet, "/api/v1/admin/runtime/summary", ""))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "go1.25") {
		t.Fatalf("summary response: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.HandleList(recorder, ownerRequest(http.MethodGet, "/api/v1/admin/runtime/captures", ""))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"busy":true`) {
		t.Fatalf("list response: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(audits) != 0 {
		t.Fatalf("read routes must not audit: %v", audits)
	}
}

func TestRuntimeDiagnosticsRequiresOwnerPrincipal(t *testing.T) {
	handler := NewRuntimeDiagnosticsHandler(&fakeRuntimeDiagnostics{}, nil)
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"summary": handler.HandleSummary, "list": handler.HandleList, "create": handler.HandleCreate,
		"download": handler.HandleDownload, "delete": handler.HandleDelete,
	} {
		recorder := httptest.NewRecorder()
		call(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/captures", nil))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", name, recorder.Code)
		}
	}
}

func TestRuntimeDiagnosticsCreateValidatesBodyAndLimits(t *testing.T) {
	fake := &fakeRuntimeDiagnostics{}
	handler := NewRuntimeDiagnosticsHandler(fake, nil)
	for name, body := range map[string]string{
		"malformed":     "{",
		"unknown field": `{"kind":"cpu","duration_seconds":5,"debug":2}`,
	} {
		recorder := httptest.NewRecorder()
		handler.HandleCreate(recorder, ownerRequest(http.MethodPost, "/api/v1/admin/runtime/captures", body))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}
	if len(fake.started) != 0 {
		t.Fatalf("rejected bodies reached the service: %v", fake.started)
	}
}

func TestRuntimeDiagnosticsCreateMapsFailureCodes(t *testing.T) {
	for code, status := range map[string]int{
		runtimediag.CodeInvalidKind:     http.StatusBadRequest,
		runtimediag.CodeInvalidDuration: http.StatusBadRequest,
		runtimediag.CodeConflict:        http.StatusConflict,
		runtimediag.CodeRateLimited:     http.StatusTooManyRequests,
		runtimediag.CodeCapacity:        http.StatusServiceUnavailable,
	} {
		fake := &fakeRuntimeDiagnostics{startErr: runtimediag.Error{Code: code}}
		handler := NewRuntimeDiagnosticsHandler(fake, nil)
		recorder := httptest.NewRecorder()
		handler.HandleCreate(recorder, ownerRequest(http.MethodPost, "/api/v1/admin/runtime/captures", `{"kind":"cpu","duration_seconds":5}`))
		if recorder.Code != status {
			t.Fatalf("%s: got %d want %d", code, recorder.Code, status)
		}
	}
}

func TestRuntimeDiagnosticsCreateAuditsAcceptedCapture(t *testing.T) {
	fake := &fakeRuntimeDiagnostics{}
	var action, resource string
	handler := NewRuntimeDiagnosticsHandler(fake, func(_ *http.Request, recorded, id string) { action, resource = recorded, id })
	recorder := httptest.NewRecorder()
	handler.HandleCreate(recorder, ownerRequest(http.MethodPost, "/api/v1/admin/runtime/captures",
		`{"kind":"trace","duration_seconds":2}`))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s", recorder.Code, recorder.Body.String())
	}
	if action != "runtime.capture.start" || resource != "0123456789abcdef" {
		t.Fatalf("audit: %q %q", action, resource)
	}
	if len(fake.started) != 1 || fake.started[0] != runtimediag.KindTrace {
		t.Fatalf("started: %v", fake.started)
	}
}

func TestRuntimeDiagnosticsDownloadSetsHeadersAndAudits(t *testing.T) {
	fake := &fakeRuntimeDiagnostics{content: "profile-bytes"}
	var audited string
	handler := NewRuntimeDiagnosticsHandler(fake, func(_ *http.Request, action, _ string) { audited = action })
	request := ownerRequest(http.MethodGet, "/api/v1/admin/runtime/captures/0123456789abcdef/download", "")
	request.SetPathValue("id", "0123456789abcdef")
	recorder := httptest.NewRecorder()
	handler.HandleDownload(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "profile-bytes" {
		t.Fatalf("download: %d %q", recorder.Code, recorder.Body.String())
	}
	headers := recorder.Result().Header
	if headers.Get("Content-Type") != "application/octet-stream" || headers.Get("Cache-Control") != "no-store" ||
		headers.Get("X-Content-Type-Options") != "nosniff" ||
		headers.Get("Content-Disposition") != `attachment; filename="cpu-0123456789abcdef.prof"` {
		t.Fatalf("headers: %v", headers)
	}
	if audited != "runtime.capture.download" {
		t.Fatalf("audit: %q", audited)
	}
}

func TestRuntimeDiagnosticsDownloadAndDeleteFailures(t *testing.T) {
	fake := &fakeRuntimeDiagnostics{downloadErr: runtimediag.Error{Code: runtimediag.CodeNotFound},
		cancelErr: runtimediag.Error{Code: runtimediag.CodeNotFound}}
	handler := NewRuntimeDiagnosticsHandler(fake, nil)
	request := ownerRequest(http.MethodGet, "/api/v1/admin/runtime/captures/missing/download", "")
	recorder := httptest.NewRecorder()
	handler.HandleDownload(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("download: %d", recorder.Code)
	}
	request = ownerRequest(http.MethodDelete, "/api/v1/admin/runtime/captures/missing", "")
	recorder = httptest.NewRecorder()
	handler.HandleDelete(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("delete: %d", recorder.Code)
	}

	fake = &fakeRuntimeDiagnostics{downloadErr: runtimediag.Error{Code: runtimediag.CodeConflict}}
	handler = NewRuntimeDiagnosticsHandler(fake, nil)
	request = ownerRequest(http.MethodGet, "/api/v1/admin/runtime/captures/running/download", "")
	recorder = httptest.NewRecorder()
	handler.HandleDownload(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("download conflict: %d", recorder.Code)
	}
}

func TestRuntimeDiagnosticsDeleteAuditsCancellation(t *testing.T) {
	fake := &fakeRuntimeDiagnostics{}
	var action, resource string
	handler := NewRuntimeDiagnosticsHandler(fake, func(_ *http.Request, recorded, id string) { action, resource = recorded, id })
	request := ownerRequest(http.MethodDelete, "/api/v1/admin/runtime/captures/abc", "")
	request.SetPathValue("id", "abc")
	recorder := httptest.NewRecorder()
	handler.HandleDelete(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", recorder.Code, recorder.Body.String())
	}
	if action != "runtime.capture.cancel" || resource != "abc" || len(fake.cancelled) != 1 {
		t.Fatalf("cancel: %q %q %v", action, resource, fake.cancelled)
	}
}

func TestRuntimeFileNameRejectsUnsafeValues(t *testing.T) {
	if name := runtimeFileName(runtimediag.Capture{Kind: "cpu", ID: "../../etc/passwd"}); name != "capture.prof" {
		t.Fatalf("unsafe id: %q", name)
	}
	if name := runtimeFileName(runtimediag.Capture{Kind: "custom", ID: "abc"}); name != "capture.prof" {
		t.Fatalf("unsupported kind: %q", name)
	}
}
