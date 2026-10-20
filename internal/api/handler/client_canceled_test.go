package handler

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

// TestHandleListGroupsReportsClientCanceledAs499 覆盖线上真实场景：浏览器刷新或
// 取消 Promise.all 时 /api/v1/groups 的读取会被取消，存储层回 `context canceled`。
// 它不是内部故障，不能记成 500，也不该占一条 WARN。
func TestHandleListGroupsReportsClientCanceledAs499(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	s := setupTestDB(t)
	handler := NewGroupHandler(store.NewGroupStore(s.DB))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	handler.HandleListGroups(recorder, request)

	if expected, actual := StatusClientClosedRequest, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("client cancellation must not warn, log = %q", logs.String())
	}
	if !strings.Contains(logs.String(), "list groups canceled by client") {
		t.Fatalf("expected a diagnostic cancellation log, log = %q", logs.String())
	}
}

// TestHandleListGroupsKeepsInternalFailureAs500 保证归一化没有放宽真正的内部故障：
// 存储不可用时仍然是 500，并且留一条 WARN。
func TestHandleListGroupsKeepsInternalFailureAs500(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	s := setupTestDB(t)
	handler := NewGroupHandler(store.NewGroupStore(s.DB))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
	recorder := httptest.NewRecorder()

	handler.HandleListGroups(recorder, request)

	if expected, actual := http.StatusInternalServerError, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("internal failure must warn, log = %q", logs.String())
	}
}
