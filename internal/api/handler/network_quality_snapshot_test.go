package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// publicQualityCall 调用公共网络质量处理器，可选带 If-None-Match。
func publicQualityCall(t *testing.T, fixture networkHandlerFixture, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	request := networkHandlerRequest(http.MethodGet, "/api/v1/network/quality", "")
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}
	response := httptest.NewRecorder()
	fixture.handler.HandlePublicNetworkQuality(response, request)
	return response
}

// publicQualityTask 是存储层直接写入用的合法公共探测任务。
func publicQualityTask(name string) model.NetworkTask {
	return model.NetworkTask{
		Name: name, Type: "icmp", Target: "127.0.0.1", IPFamily: "auto",
		IntervalSeconds: 60, TimeoutMilliseconds: 1000, AllNodes: true, Enabled: true, IsPublic: true,
	}
}

func TestPublicNetworkQualitySnapshotReusesUntilWindowExpires(t *testing.T) {
	fixture := setupNetworkHandler(t)
	current := fixture.now
	fixture.handler.now = func() time.Time { return current }
	created := createHandlerTask(t, fixture, true, true)

	first := publicQualityCall(t, fixture, "")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), created.ID) {
		t.Fatalf("first quality status = %d, body = %s", first.Code, first.Body.String())
	}
	if etag := first.Header().Get("ETag"); etag == "" {
		t.Fatal("public quality response has no validator")
	}

	// 直接经存储写入第二个任务：不经过处理器，视图代不变，因此窗口内仍复用上一代视图。
	added, err := fixture.tasks.CreateTask(t.Context(), publicQualityTask("Later"), nil)
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	cached := publicQualityCall(t, fixture, "")
	if cached.Body.String() != first.Body.String() {
		t.Fatalf("expected the shared snapshot within the window, got %s", cached.Body.String())
	}
	if cached.Header().Get("ETag") != first.Header().Get("ETag") {
		t.Fatal("reused snapshot changed its validator")
	}

	current = current.Add(publicNetworkQualityReuseWindow + time.Second)
	rebuilt := publicQualityCall(t, fixture, "")
	if rebuilt.Body.String() == first.Body.String() {
		t.Fatal("expected a rebuild after the reuse window")
	}
	if !strings.Contains(rebuilt.Body.String(), added.ID) {
		t.Fatalf("rebuilt snapshot misses the task written out of band: %s", rebuilt.Body.String())
	}
	if rebuilt.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatal("rebuilt snapshot kept the previous validator")
	}
}

func TestPublicNetworkQualitySnapshotInvalidatesOnWrites(t *testing.T) {
	fixture := setupNetworkHandler(t)
	empty := publicQualityCall(t, fixture, "")
	if empty.Code != http.StatusOK || strings.Contains(empty.Body.String(), `"id":"`) {
		t.Fatalf("empty quality status = %d, body = %s", empty.Code, empty.Body.String())
	}
	if generation := fixture.handler.qualityGeneration.Load(); generation != 1 {
		t.Fatalf("quality generation after first build = %d, want 1", generation)
	}

	created := createHandlerTask(t, fixture, true, true)
	afterCreate := publicQualityCall(t, fixture, "")
	if !strings.Contains(afterCreate.Body.String(), created.ID) {
		t.Fatalf("task write did not invalidate the snapshot: %s", afterCreate.Body.String())
	}

	postNetworkResult(t, fixture, created.ID, fixture.now)
	afterResult := publicQualityCall(t, fixture, "")
	if !strings.Contains(afterResult.Body.String(), `"latency_ms":1.5`) {
		t.Fatalf("probe result did not invalidate the snapshot: %s", afterResult.Body.String())
	}

	update := networkHandlerRequest(http.MethodPut, "/tasks/"+created.ID,
		validTaskBody(true, true, true, `,"node_ids":["`+fixture.node.ID+`"]`))
	update.SetPathValue("task_id", created.ID)
	assertHandlerStatus(t, fixture.handler.HandleUpdateNetworkTask, update, http.StatusOK)
	afterUpdate := publicQualityCall(t, fixture, "")
	if !strings.Contains(afterUpdate.Body.String(), fixture.node.ID) {
		t.Fatalf("task update did not invalidate the snapshot: %s", afterUpdate.Body.String())
	}

	sortRequest := networkHandlerRequest(http.MethodPut, "/tasks/sort", `{"ids":["`+created.ID+`"]}`)
	assertHandlerStatus(t, fixture.handler.HandleSortNetworkTasks, sortRequest, http.StatusOK)
	sortGeneration := fixture.handler.qualityGeneration.Load()
	assertHandlerStatus(t, fixture.handler.HandleDeleteNetworkTask,
		requestWithTaskID(http.MethodDelete, created.ID, asyncDeletionBody(created.Name)), http.StatusAccepted)
	if fixture.handler.qualityGeneration.Load() <= sortGeneration {
		t.Fatal("task deletion did not invalidate the snapshot")
	}
}

func TestPublicNetworkQualitySnapshotHonoursIfNoneMatch(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)
	first := publicQualityCall(t, fixture, "")
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("validator headers = %q, %q", etag, first.Header().Get("Cache-Control"))
	}

	notModified := publicQualityCall(t, fixture, etag)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional status = %d, body = %s", notModified.Code, notModified.Body.String())
	}

	stale := publicQualityCall(t, fixture, `W/"public-network-quality-0"`)
	if stale.Code != http.StatusOK || !strings.Contains(stale.Body.String(), created.ID) {
		t.Fatalf("stale validator status = %d, body = %s", stale.Code, stale.Body.String())
	}
}

func TestPublicNetworkQualitySnapshotCoalescesConcurrentViewers(t *testing.T) {
	fixture := setupNetworkHandler(t)
	createHandlerTask(t, fixture, true, true)
	generation := fixture.handler.qualityGeneration.Load()
	bodies := make([]string, 8)
	var wait sync.WaitGroup
	for index := range bodies {
		wait.Add(1)
		go func() {
			defer wait.Done()
			bodies[index] = publicQualityCall(t, fixture, "").Body.String()
		}()
	}
	wait.Wait()
	for index, body := range bodies {
		if body != bodies[0] {
			t.Fatalf("viewer %d saw a different snapshot: %s", index, body)
		}
	}
	if built := fixture.handler.qualityGeneration.Load(); built != generation+1 {
		t.Fatalf("quality generation = %d, want a single build after %d", built, generation)
	}
}

func TestPublicNetworkQualitySnapshotKeepsTheLastViewOnFailure(t *testing.T) {
	fixture := setupNetworkHandler(t)
	current := fixture.now
	fixture.handler.now = func() time.Time { return current }
	_ = createHandlerTask(t, fixture, true, true)
	before := publicQualityCall(t, fixture, "")
	if before.Code != http.StatusOK {
		t.Fatalf("first quality status = %d", before.Code)
	}
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatalf("close SQLite: %v", err)
	}

	// 窗口内继续复用上一份可用视图：失败构建不会清掉它。
	reused := publicQualityCall(t, fixture, "")
	if reused.Code != http.StatusOK || reused.Body.String() != before.Body.String() {
		t.Fatalf("reused after failure = %d, body = %s", reused.Code, reused.Body.String())
	}

	current = current.Add(publicNetworkQualityReuseWindow + time.Second)
	failed := publicQualityCall(t, fixture, "")
	if failed.Code != http.StatusInternalServerError ||
		!strings.Contains(failed.Body.String(), "failed to list network quality") {
		t.Fatalf("failed rebuild = %d, body = %s", failed.Code, failed.Body.String())
	}
}
