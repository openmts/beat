package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const (
	metricErasureHandlerNodeID  = "22222222-2222-4222-8222-222222222222"
	metricErasureHandlerRequest = "aaaaaaaa-1111-4111-8111-111111111111"
	metricErasureHandlerSecond  = "bbbbbbbb-2222-4222-8222-222222222222"
	metricErasureHandlerMissing = "cccccccc-3333-4333-8333-333333333333"
)

type metricErasureHandlerFixture struct {
	handler *MetricErasureHandler
	jobs    *store.MetricErasureStore
	sqlite  *store.SQLiteStore
	now     time.Time
	signals int
}

func newMetricErasureHandlerFixture(t *testing.T) *metricErasureHandlerFixture {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "erasure-handler.db"))
	if err != nil {
		t.Fatalf("new erasure handler store: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	now := time.Date(2026, time.October, 16, 9, 30, 0, 0, time.UTC)
	if _, err := sqlite.DB.ExecContext(t.Context(),
		"INSERT INTO nodes (id,name,host,port,created_at,updated_at) VALUES (?,?,?,?,?,?)",
		metricErasureHandlerNodeID, "erasure-target", "192.0.2.9", 22, now, now); err != nil {
		t.Fatalf("seed erasure target node: %v", err)
	}
	fixture := &metricErasureHandlerFixture{jobs: store.NewMetricErasureStore(sqlite.DB), sqlite: sqlite, now: now}
	fixture.handler = NewMetricErasureHandler(fixture.jobs).
		WithClock(func() time.Time { return now }).
		WithSignal(func() { fixture.signals += 1 })
	return fixture
}

// callMetricErasureHandler 直接驱动处理器，路由中间件不在本层用例范围内。
func callMetricErasureHandler(
	t *testing.T,
	handle func(http.ResponseWriter, *http.Request),
	method string,
	path string,
	body string,
	id string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if id != "" {
		request.SetPathValue("job_id", id)
	}
	response := httptest.NewRecorder()
	handle(response, request)
	return response
}

func (fixture *metricErasureHandlerFixture) createFleetJob(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return callMetricErasureHandler(t, fixture.handler.HandleCreate, http.MethodPost,
		"/api/v1/admin/metric-erasure/jobs", body, "")
}

func TestMetricErasureHandlerCreatesListsAndReadsJobs(t *testing.T) {
	fixture := newMetricErasureHandlerFixture(t)
	created := fixture.createFleetJob(t, `{"scope":"fleet_telemetry","request_id":"`+
		metricErasureHandlerRequest+`","confirmation":"ERASE FLEET TELEMETRY"}`)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"replayed":false`) {
		t.Fatalf("create body = %s", created.Body.String())
	}
	if fixture.signals != 1 {
		t.Fatalf("signals after create = %d", fixture.signals)
	}
	var created_ metricErasureCreateResponse
	if err := decodeMetricErasureBody(t, created, &created_); err != nil {
		t.Fatal(err)
	}
	if created_.Job.Cutoff != nil || created_.Job.State != model.MetricErasureQueued ||
		created_.Job.Scope != model.MetricErasureFleetTelemetry || created_.Job.Attempts != 0 {
		t.Fatalf("created job view = %+v", created_.Job)
	}

	listed := callMetricErasureHandler(t, fixture.handler.HandleList, http.MethodGet,
		"/api/v1/admin/metric-erasure/jobs", "", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d", listed.Code)
	}
	var page metricErasureListResponse
	if err := decodeMetricErasureBody(t, listed, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Jobs) != 1 || page.Jobs[0].ID != created_.Job.ID {
		t.Fatalf("listed jobs = %+v", page.Jobs)
	}

	// 冻结截止时间后视图必须暴露 RFC3339 时间戳，方便前端展示删除边界。
	cutoff := time.Date(2026, time.October, 16, 10, 0, 0, 0, time.UTC)
	if _, err := fixture.sqlite.DB.ExecContext(t.Context(),
		"UPDATE metric_erasure_jobs SET cutoff_unix_nano=?, started_at=?, measurement_cursor=? WHERE id=?",
		cutoff.UnixNano(), fixture.now, "cpu", created_.Job.ID); err != nil {
		t.Fatalf("freeze erasure cutoff: %v", err)
	}
	read := callMetricErasureHandler(t, fixture.handler.HandleGet, http.MethodGet,
		"/api/v1/admin/metric-erasure/jobs/"+created_.Job.ID, "", created_.Job.ID)
	if read.Code != http.StatusOK {
		t.Fatalf("get status = %d body = %s", read.Code, read.Body.String())
	}
	var detail metricErasureView
	if err := decodeMetricErasureBody(t, read, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Cutoff == nil || !detail.Cutoff.Equal(cutoff) || detail.StartedAt == nil ||
		detail.ProgressCursor != "cpu" {
		t.Fatalf("job detail = %+v", detail)
	}
}

func TestMetricErasureHandlerCreateReplaysAndRejectsInvalidRequests(t *testing.T) {
	fixture := newMetricErasureHandlerFixture(t)
	body := `{"scope":"fleet_telemetry","request_id":"` + metricErasureHandlerRequest +
		`","confirmation":"ERASE FLEET TELEMETRY"}`
	if response := fixture.createFleetJob(t, body); response.Code != http.StatusAccepted {
		t.Fatalf("first create status = %d", response.Code)
	}
	replay := fixture.createFleetJob(t, body)
	if replay.Code != http.StatusAccepted || !strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatalf("replay status = %d body = %s", replay.Code, replay.Body.String())
	}
	if fixture.signals != 1 {
		t.Fatalf("signals after replay = %d", fixture.signals)
	}
	// 舰队级擦除互斥：第二个未完成任务必须被拒绝而不是排队。
	conflict := fixture.createFleetJob(t, `{"scope":"fleet_telemetry","request_id":"`+
		metricErasureHandlerSecond+`","confirmation":"ERASE FLEET TELEMETRY"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body = %s", conflict.Code, conflict.Body.String())
	}
	wrongPhrase := fixture.createFleetJob(t, `{"scope":"all_metric_history","request_id":"`+
		metricErasureHandlerMissing+`","confirmation":"ERASE ALL"}`)
	if wrongPhrase.Code != http.StatusBadRequest {
		t.Fatalf("wrong phrase status = %d body = %s", wrongPhrase.Code, wrongPhrase.Body.String())
	}
	malformed := fixture.createFleetJob(t, "{")
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d body = %s", malformed.Code, malformed.Body.String())
	}
	missingTarget := fixture.createFleetJob(t, `{"scope":"node_history","target_id":"`+
		metricErasureHandlerMissing+`","request_id":"`+metricErasureHandlerMissing+
		`","confirmation":"erasure-target"}`)
	if missingTarget.Code != http.StatusNotFound {
		t.Fatalf("missing target status = %d body = %s", missingTarget.Code, missingTarget.Body.String())
	}
}

func TestMetricErasureHandlerValidatesListLimitAndMissingJobs(t *testing.T) {
	fixture := newMetricErasureHandlerFixture(t)
	for _, raw := range []string{"abc", "0", "-3"} {
		response := callMetricErasureHandler(t, fixture.handler.HandleList, http.MethodGet,
			"/api/v1/admin/metric-erasure/jobs?limit="+raw, "", "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("limit %q status = %d body = %s", raw, response.Code, response.Body.String())
		}
	}
	// 显式上限与空查询串都走同一条读取路径。
	accepted := callMetricErasureHandler(t, fixture.handler.HandleList, http.MethodGet,
		"/api/v1/admin/metric-erasure/jobs?limit=5", "", "")
	if accepted.Code != http.StatusOK {
		t.Fatalf("explicit limit status = %d", accepted.Code)
	}
	missing := callMetricErasureHandler(t, fixture.handler.HandleGet, http.MethodGet,
		"/api/v1/admin/metric-erasure/jobs/"+metricErasureHandlerMissing, "", metricErasureHandlerMissing)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing job status = %d body = %s", missing.Code, missing.Body.String())
	}
	missingAction := callMetricErasureHandler(t, fixture.handler.HandleCancel, http.MethodPost,
		"/api/v1/admin/metric-erasure/jobs/"+metricErasureHandlerMissing+"/cancel", "", metricErasureHandlerMissing)
	if missingAction.Code != http.StatusNotFound {
		t.Fatalf("missing cancel status = %d body = %s", missingAction.Code, missingAction.Body.String())
	}
}

func TestMetricErasureHandlerCancelsAndRetriesJobs(t *testing.T) {
	fixture := newMetricErasureHandlerFixture(t)
	created := fixture.createFleetJob(t, `{"scope":"fleet_telemetry","request_id":"`+
		metricErasureHandlerRequest+`","confirmation":"ERASE FLEET TELEMETRY"}`)
	var body metricErasureCreateResponse
	if err := decodeMetricErasureBody(t, created, &body); err != nil {
		t.Fatal(err)
	}
	// 只有失败的擦除任务可以重试；排队中的任务重试是冲突而不是静默忽略。
	early := callMetricErasureHandler(t, fixture.handler.HandleRetry, http.MethodPost,
		"/api/v1/admin/metric-erasure/jobs/"+body.Job.ID+"/retry", "", body.Job.ID)
	if early.Code != http.StatusConflict {
		t.Fatalf("early retry status = %d body = %s", early.Code, early.Body.String())
	}
	canceled := callMetricErasureHandler(t, fixture.handler.HandleCancel, http.MethodPost,
		"/api/v1/admin/metric-erasure/jobs/"+body.Job.ID+"/cancel", "", body.Job.ID)
	if canceled.Code != http.StatusOK || !strings.Contains(canceled.Body.String(), `"state":"canceled"`) {
		t.Fatalf("cancel status = %d body = %s", canceled.Code, canceled.Body.String())
	}
	if fixture.signals != 2 {
		t.Fatalf("signals after cancel = %d", fixture.signals)
	}
	if _, err := fixture.sqlite.DB.ExecContext(t.Context(),
		"UPDATE metric_erasure_jobs SET state='failed', last_error_code=? WHERE id=?",
		model.MetricErasureErrorMTS, body.Job.ID); err != nil {
		t.Fatalf("mark erasure job failed: %v", err)
	}
	retried := callMetricErasureHandler(t, fixture.handler.HandleRetry, http.MethodPost,
		"/api/v1/admin/metric-erasure/jobs/"+body.Job.ID+"/retry", "", body.Job.ID)
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), `"state":"queued"`) {
		t.Fatalf("retry status = %d body = %s", retried.Code, retried.Body.String())
	}
	if fixture.signals != 3 {
		t.Fatalf("signals after retry = %d", fixture.signals)
	}
}

func TestMetricErasureHandlerReportsUnavailableAndInternalFailures(t *testing.T) {
	// 读路径容忍 nil 处理器；管理动作在 jobs 为 nil 时同样返回 503 而不是空指针。
	var missing *MetricErasureHandler
	empty := &MetricErasureHandler{}
	entries := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"list", missing.HandleList},
		{"get", missing.HandleGet},
		{"create", missing.HandleCreate},
		{"retry", empty.HandleRetry},
		{"cancel", empty.HandleCancel},
	}
	for _, entry := range entries {
		response := callMetricErasureHandler(t, entry.call, http.MethodGet, "/api/v1/admin/metric-erasure/jobs", "", "")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d body = %s", entry.name, response.Code, response.Body.String())
		}
	}

	fixture := newMetricErasureHandlerFixture(t)
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatalf("close erasure store: %v", err)
	}
	failed := callMetricErasureHandler(t, fixture.handler.HandleList, http.MethodGet,
		"/api/v1/admin/metric-erasure/jobs", "", "")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("closed store status = %d body = %s", failed.Code, failed.Body.String())
	}
}

func TestMetricErasureHandlerErrorMapping(t *testing.T) {
	fixture := newMetricErasureHandlerFixture(t)
	cases := []struct {
		name    string
		err     error
		status  int
		handled bool
	}{
		{"nil", nil, 0, false},
		{"forbidden", store.ErrMetadataForbidden, http.StatusForbidden, true},
		{"recent-auth", store.ErrMetadataRecentAuth, http.StatusPreconditionRequired, true},
		{"invalid", model.ErrMetricErasureInvalid, http.StatusBadRequest, true},
		{"conflict", model.ErrMetricErasureConflict, http.StatusConflict, true},
		{"missing", model.ErrMetricErasureMissing, http.StatusNotFound, true},
		{"unknown", errors.New("erasure backend exploded"), http.StatusInternalServerError, true},
	}
	for _, testCase := range cases {
		response := httptest.NewRecorder()
		if handled := fixture.handler.respondError(response, testCase.err); handled != testCase.handled {
			t.Fatalf("%s handled = %v", testCase.name, handled)
		}
		if testCase.status != 0 && response.Code != testCase.status {
			t.Fatalf("%s status = %d body = %s", testCase.name, response.Code, response.Body.String())
		}
	}
}

func TestMetricErasureViewProjectsCutoffOnlyWhenPresent(t *testing.T) {
	cutoff := int64(1_700_000_000_000_000_000)
	view := newMetricErasureView(model.MetricErasureJob{ID: "job", Scope: model.MetricErasureNodeHistory,
		TargetType: model.MetricErasureTargetNode, CutoffUnixNano: &cutoff})
	if view.Cutoff == nil || view.Cutoff.UnixNano() != cutoff {
		t.Fatalf("cutoff projection = %+v", view.Cutoff)
	}
	empty := newMetricErasureView(model.MetricErasureJob{ID: "job"})
	if empty.Cutoff != nil {
		t.Fatalf("unexpected cutoff = %+v", empty.Cutoff)
	}
}

// decodeMetricErasureBody 解开统一响应信封后解码 data 字段。
func decodeMetricErasureBody(t *testing.T, response *httptest.ResponseRecorder, target any) error {
	t.Helper()
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		return err
	}
	if envelope.Code != response.Code {
		return fmt.Errorf("envelope code %d, response code %d", envelope.Code, response.Code)
	}
	return json.Unmarshal(envelope.Data, target)
}
