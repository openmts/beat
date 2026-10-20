package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type stubPublicSnapshot struct {
	value publicSnapshot
	ok    bool
	calls int
}

func (stub *stubPublicSnapshot) FreshPublicNodes() (publicSnapshot, bool) {
	stub.calls++
	return stub.value, stub.ok
}

func snapshotNode(id, group string) publicNodeResponse {
	return publicNodeResponse{nodeResponse: nodeResponse{Node: model.Node{ID: id, Name: id, GroupID: group}}}
}

func TestPublicNodeSnapshotServesListAndDetail(t *testing.T) {
	source := &stubPublicSnapshot{ok: true, value: publicSnapshot{Generation: 7, Timestamp: time.Now(), AcceleratorsVisible: true, Nodes: []publicNodeResponse{
		snapshotNode("node-a", "group-one"), snapshotNode("node-b", "group-two"),
	}}}
	handler := (&NodeHandler{}).WithPublicSnapshot(source)

	list := httptest.NewRecorder()
	handler.HandleListNodes(list, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	if expected, actual := http.StatusOK, list.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if expected, actual := `W/"public-nodes-7"`, list.Header().Get("ETag"); expected != actual {
		t.Fatalf("expected etag %s, actual %s", expected, actual)
	}
	if body := list.Body.String(); !contains(body, "node-a") || !contains(body, "node-b") {
		t.Fatalf("unexpected list body %s", body)
	}

	filtered := httptest.NewRecorder()
	handler.HandleListNodes(filtered, httptest.NewRequest(http.MethodGet, "/api/v1/nodes?group_id=group-one", nil))
	if contains(filtered.Body.String(), "node-b") || !contains(filtered.Body.String(), "node-a") {
		t.Fatalf("group filter leaked other nodes: %s", filtered.Body.String())
	}

	detail := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-b", nil)
	request.SetPathValue("id", "node-b")
	handler.HandleGetNode(detail, request)
	if expected, actual := http.StatusOK, detail.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if !contains(detail.Body.String(), "node-b") {
		t.Fatalf("unexpected detail body %s", detail.Body.String())
	}
}

func TestPublicNodeSnapshotRejectsGlobalVisibilityMismatch(t *testing.T) {
	source := &stubPublicSnapshot{ok: true, value: publicSnapshot{Generation: 8, Timestamp: time.Now(), AcceleratorsVisible: true}}
	handler := (&NodeHandler{}).WithPublicSnapshot(source)
	if _, ok := handler.cachedPublicSnapshot(false); ok {
		t.Fatal("snapshot built with accelerators visible must not serve a hidden request")
	}
	source.value.AcceleratorsVisible = false
	if _, ok := handler.cachedPublicSnapshot(false); !ok {
		t.Fatal("snapshot with matching global visibility must be reusable")
	}
}

func TestPublicNodeSnapshotHonorsConditionalGet(t *testing.T) {
	source := &stubPublicSnapshot{ok: true, value: publicSnapshot{Generation: 3, Timestamp: time.Now(), AcceleratorsVisible: true, Nodes: []publicNodeResponse{snapshotNode("node-a", "")}}}
	handler := (&NodeHandler{}).WithPublicSnapshot(source)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	request.Header.Set("If-None-Match", publicNodeETag(3))
	recorder := httptest.NewRecorder()
	handler.HandleListNodes(recorder, request)
	if expected, actual := http.StatusNotModified, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("304 must not carry a body, actual %s", recorder.Body.String())
	}
}

func TestPublicNodeSnapshotMissingNodeIsNotFoundAndFallsBackWhenStale(t *testing.T) {
	source := &stubPublicSnapshot{ok: true, value: publicSnapshot{Generation: 1, Timestamp: time.Now(), AcceleratorsVisible: true, Nodes: []publicNodeResponse{snapshotNode("node-a", "")}}}
	handler := (&NodeHandler{}).WithPublicSnapshot(source)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/missing", nil)
	request.SetPathValue("id", "missing")
	recorder := httptest.NewRecorder()
	handler.HandleGetNode(recorder, request)
	if expected, actual := http.StatusNotFound, recorder.Code; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}

	stale := &stubPublicSnapshot{ok: false}
	fallback := (&NodeHandler{}).WithPublicSnapshot(stale)
	if _, ok := fallback.cachedPublicSnapshot(true); ok {
		t.Fatal("stale snapshot must not be used")
	}
	if expected, actual := 1, stale.calls; expected != actual {
		t.Fatalf("expected one source lookup, actual %d", actual)
	}
}

func TestFreshPublicNodesExpireOutsideStaleWindow(t *testing.T) {
	handler := &MetricsHandler{}
	if _, ok := handler.FreshPublicNodes(); ok {
		t.Fatal("empty handler must not report a snapshot")
	}
	handler.storePublicSnapshot(metricsSnapshot{at: model.NowUTC(), AcceleratorsVisible: true, Nodes: []publicNodeResponse{snapshotNode("node-a", "")}})
	value, ok := handler.FreshPublicNodes()
	if !ok || value.Generation != 1 || len(value.Nodes) != 1 {
		t.Fatalf("expected a fresh generation-1 snapshot, actual %+v ok=%v", value, ok)
	}
	// storePublicSnapshot 以构建完成时刻计时，因此这里直接写入时间戳陈旧的快照来验证过期判定。
	handler.latest.Store(&publicSnapshot{Generation: 2, Timestamp: model.NowUTC().Add(-2 * publicSnapshotReuseWindow), AcceleratorsVisible: true})
	if _, ok := handler.FreshPublicNodes(); ok {
		t.Fatal("expired snapshot must not be reported as fresh")
	}
}

func TestFilterPublicNodesRefreshesTelemetryAge(t *testing.T) {
	at := model.NowUTC().Add(-2 * time.Minute)
	node := snapshotNode("node-a", "")
	node.TelemetryAt = &at
	node.TelemetryAgeSeconds = pointer(int64(1))
	refreshed := filterPublicNodes([]publicNodeResponse{node}, "", model.NowUTC())
	if refreshed[0].TelemetryAgeSeconds == nil || *refreshed[0].TelemetryAgeSeconds < 100 {
		t.Fatalf("expected a refreshed age near 120s, actual %v", refreshed[0].TelemetryAgeSeconds)
	}
	if node.TelemetryAgeSeconds == nil || *node.TelemetryAgeSeconds != 1 {
		t.Fatal("refresh must not mutate the shared snapshot value")
	}
}

func pointer[T any](value T) *T { return &value }

func TestFilterPublicNodesKeepsEmptyResultAsJSONArray(t *testing.T) {
	filtered := filterPublicNodes([]publicNodeResponse{snapshotNode("node-a", "group-one")}, "missing", model.NowUTC())
	if filtered == nil || len(filtered) != 0 {
		t.Fatalf("expected an empty non-nil slice, actual %#v", filtered)
	}
	if _, found := findPublicNode(nil, "node-a"); found {
		t.Fatal("missing node must not be found")
	}
}

func contains(value, needle string) bool {
	return len(needle) > 0 && len(value) >= len(needle) && (func() bool {
		for index := 0; index+len(needle) <= len(value); index++ {
			if value[index:index+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
