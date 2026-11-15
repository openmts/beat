package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"beat/internal/auth"
	"beat/internal/config"
	"beat/internal/domain"
	"beat/internal/platform/sqlite"
	"beat/internal/platform/tsdb"
	beatv1 "beat/internal/protocol/beatv1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestTaskCenterRoutes(t *testing.T) {
	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		cfg:         config.Config{Listen: ":9099", EnrollmentTokenTTL: 10 * time.Minute},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:       store,
		authManager: manager,
	}
	router := server.routes()

	response := serveRequest(router, http.MethodGet, "/api/v1/tasks", "", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous task list status = %d, want 401", response.Code)
	}
	response = serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"admin"}`)
	cookies := response.Result().Cookies()
	if response.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("login = %d, cookies=%+v: %s", response.Code, cookies, response.Body.String())
	}
	cookie := cookies[0].Name + "=" + cookies[0].Value
	var loginPayload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &loginPayload); err != nil || loginPayload.CSRFToken == "" {
		t.Fatalf("login CSRF token: %v %s", err, response.Body.String())
	}
	csrfToken := loginPayload.CSRFToken

	response = serveRequest(router, http.MethodGet, "/api/v1/tasks", cookie, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":0`) {
		t.Fatalf("task list = %d: %s", response.Code, response.Body.String())
	}

	response = serveAdminRequest(router, http.MethodPost, "/api/v1/tasks", cookie, csrfToken, `{"name":"Check API","description":"custom","script":"printf ok","intervalSeconds":60,"timeoutSeconds":10,"enabled":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create task = %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"source":"custom"`) || !strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatalf("unexpected created task: %s", response.Body.String())
	}
	var created domain.TaskDefinition
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	response = serveAdminRequest(router, http.MethodPatch, "/api/v1/tasks/"+created.ID, cookie, csrfToken, `{"script":"printf task-updated"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "printf task-updated") {
		t.Fatalf("edit task script = %d: %s", response.Code, response.Body.String())
	}

	networkConfig := `{"targets":[{"carrier":"telecom","label":"电信","host":"127.0.0.1","port":9099,"protocol":"tcp","addressFamily":"auto"},{"carrier":"unicom","label":"联通","host":"127.0.0.1","port":9099,"protocol":"tcp","addressFamily":"auto"},{"carrier":"mobile","label":"移动","host":"127.0.0.1","port":9099,"protocol":"tcp","addressFamily":"auto"}],"probe":{"count":5,"intervalMs":100,"timeoutMs":500}}`
	response = serveAdminRequest(router, http.MethodPost, "/api/v1/tasks", cookie, csrfToken, `{"name":"三网 TCP","description":"network","kind":"three_network_probe","script":"","config":`+networkConfig+`,"intervalSeconds":60,"timeoutSeconds":30,"enabled":false}`)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"kind":"three_network_probe"`) || !strings.Contains(response.Body.String(), `"host":"127.0.0.1"`) {
		t.Fatalf("create network task = %d: %s", response.Code, response.Body.String())
	}
	response = serveAdminRequest(router, http.MethodPost, "/api/v1/tasks", cookie, csrfToken, `{"name":"坏配置","kind":"three_network_probe","config":{"targets":[],"probe":{"count":1,"intervalMs":100,"timeoutMs":500}},"intervalSeconds":60,"timeoutSeconds":30,"enabled":false}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid network task = %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestAgentReportSchedulesAndCompletesTask(t *testing.T) {
	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, "shell_script", "Report check", "", "printf report-ok", json.RawMessage(`{}`), 60, 10, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	backend := &recordingTSDB{}
	server := &Server{
		cfg:           config.Config{Listen: ":9099", AgentToken: "test-agent-token", EnrollmentTokenTTL: 10 * time.Minute},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:         store,
		tsdb:          backend,
		agentSessions: newAgentSessionRegistry(),
	}
	report := domain.Report{
		AgentID:         "agent-task-test",
		Hostname:        "edge-task-test",
		IntervalSeconds: 15,
		Metrics:         domain.Metrics{Timestamp: time.Now().UTC(), CPUUsage: 12},
	}
	stream := openAgentTestStream(t, server)
	if err := stream.Send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_Hello{Hello: &beatv1.Hello{AgentId: report.AgentID, Hostname: report.Hostname, Version: "test"}}}); err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetWelcome() == nil {
		t.Fatalf("welcome = %+v, %v", first, err)
	}
	if err := stream.Send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_Report{Report: beatv1.ReportFromDomain(report)}}); err != nil {
		t.Fatal(err)
	}
	dispatchMessage, err := receiveServerPayload(t, stream)
	if err != nil {
		t.Fatal(err)
	}
	delivered := dispatchMessage.GetTaskDispatch()
	if delivered == nil || delivered.GetKind() != "shell_script" || delivered.GetTaskId() != task.ID || delivered.GetScript() != task.Script {
		t.Fatalf("unexpected delivered task: %+v", dispatchMessage)
	}
	if len(backend.appended) != 1 || backend.appended[0].CPUUsage != 12 {
		t.Fatalf("metrics were not written to TSDB: %+v", backend.appended)
	}

	if err := stream.Send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_TaskResult{TaskResult: &beatv1.TaskResult{
		RunId: delivered.GetRunId(), TaskId: task.ID, Status: "success", Output: "report-ok", ExitCode: 0, DurationMs: 25,
	}}}); err != nil {
		t.Fatal(err)
	}
	ackMessage, err := receiveServerPayload(t, stream)
	if err != nil {
		t.Fatal(err)
	}
	if ack := ackMessage.GetTaskResultAck(); ack == nil || !ack.GetAccepted() {
		t.Fatalf("unexpected task result ack: %+v", ackMessage)
	}
	runs, err := store.ListTaskRuns(ctx, task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].Output != "report-ok" || runs[0].DurationMs != 25 {
		t.Fatalf("unexpected completed run: %+v", runs)
	}
}

func TestCompleteAgentTaskResultPersistsNetworkProbeToTSDB(t *testing.T) {
	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, "three_network_probe", "三网质量巡检", "test", "", json.RawMessage(`{"targets":[]}`), 60, 10, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	runs, err := store.ScheduleAgentTaskRuns(ctx, "agent-network", "node-network", "edge-network", time.Now().UTC())
	if err != nil || len(runs) != 1 {
		t.Fatalf("ScheduleAgentTaskRuns() = %+v, %v", runs, err)
	}
	backend := &recordingTSDB{}
	server := &Server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:  store,
		tsdb:   backend,
	}
	output := `{"kind":"three_network_probe","startedAt":"2026-11-12T01:02:03Z","durationMs":1900,"targets":[{"carrier":"mobile","label":"移动目标","loss":{"available":true,"sent":3,"received":0,"lost":3,"failed":0,"percent":100},"latency":{"available":false,"sampleCount":0,"minMs":null,"avgMs":null,"maxMs":null,"p50Ms":null,"p95Ms":null,"jitterMs":null},"status":"failed"}]}`
	completed, err := server.completeAgentTaskResult(ctx, "agent-network", &beatv1.TaskResult{
		RunId: runs[0].ID, TaskId: task.ID, Status: "success", Output: output, ExitCode: 0, DurationMs: 1900,
	})
	if err != nil || !completed {
		t.Fatalf("completeAgentTaskResult() = %v, %v", completed, err)
	}
	if len(backend.networkProbes) != 1 || backend.networkProbeNodeID != "node-network" {
		t.Fatalf("network probe was not written to TSDB: node=%s points=%+v", backend.networkProbeNodeID, backend.networkProbes)
	}
	point := backend.networkProbes[0]
	if point.TaskID != task.ID || point.TaskName != task.Name || point.Carrier != "mobile" || !point.LossAvailable || point.LossPercent != 100 || point.LatencyAvailable {
		t.Fatalf("unexpected network probe point: %+v", point)
	}
}

func TestCompleteAgentTaskResultPersistsCustomShellMetricsToConfiguredMeasurement(t *testing.T) {
	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	configJSON := json.RawMessage(`{"metricSchema":{"enabled":true,"measurement":"app_custom_metrics","metrics":[{"key":"qps","name":"请求速率","extractor":{"type":"json_path","expression":"$.qps"},"unit":"次/秒"},{"key":"connections","name":"连接数","extractor":{"type":"json_path","expression":"$.connections"},"unit":"个"}]}}`)
	task, err := store.CreateTask(ctx, "shell_script", "应用指标", "test", `printf '{"qps":118.7,"connections":42}'`, configJSON, 60, 10, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	runs, err := store.ScheduleAgentTaskRuns(ctx, "agent-metrics", "node-metrics", "edge-metrics", time.Now().UTC())
	if err != nil || len(runs) != 1 {
		t.Fatalf("ScheduleAgentTaskRuns() = %+v, %v", runs, err)
	}
	backend := &recordingTSDB{}
	server := &Server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:  store,
		tsdb:   backend,
	}
	completed, err := server.completeAgentTaskResult(ctx, "agent-metrics", &beatv1.TaskResult{
		RunId: runs[0].ID, TaskId: task.ID, Status: "success", Output: `{"qps":118.7,"connections":42}`, ExitCode: 0, DurationMs: 25,
	})
	if err != nil || !completed {
		t.Fatalf("completeAgentTaskResult() = %v, %v", completed, err)
	}
	if backend.taskMetricMeasurement != "app_custom_metrics" || backend.taskMetricNodeID != "node-metrics" || backend.taskMetricTaskID != task.ID {
		t.Fatalf("unexpected task metric target: measurement=%s node=%s task=%s", backend.taskMetricMeasurement, backend.taskMetricNodeID, backend.taskMetricTaskID)
	}
	if backend.taskMetrics["qps"] != 118.7 || backend.taskMetrics["connections"] != 42 {
		t.Fatalf("unexpected task metrics: %+v", backend.taskMetrics)
	}
}

func openAgentTestStream(t *testing.T, server *Server) beatv1.AgentControl_ConnectClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	beatv1.RegisterAgentControlServer(grpcServer, &agentControlService{server: server})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	conn, err := grpc.NewClient("passthrough:///agent-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer test-agent-token")
	stream, err := beatv1.NewAgentControlClient(conn).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func receiveServerPayload(t *testing.T, stream beatv1.AgentControl_ConnectClient) (*beatv1.ServerMessage, error) {
	t.Helper()
	for attempts := 0; attempts < 10; attempts++ {
		message, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		if _, ok := message.Payload.(*beatv1.ServerMessage_Heartbeat); ok {
			continue
		}
		return message, nil
	}
	return nil, context.DeadlineExceeded
}

type recordingTSDB struct {
	appended              []domain.Metrics
	networkProbeNodeID    string
	networkProbes         []domain.NetworkProbePoint
	taskMetricNodeID      string
	taskMetricMeasurement string
	taskMetricTaskID      string
	taskMetricTaskName    string
	taskMetricTimestamp   time.Time
	taskMetrics           map[string]float64
}

func (t *recordingTSDB) Append(_ context.Context, _ string, metrics domain.Metrics) error {
	t.appended = append(t.appended, metrics)
	return nil
}
func (t *recordingTSDB) AppendBatch(context.Context, string, []domain.Metrics) error { return nil }
func (t *recordingTSDB) Query(context.Context, string, time.Time, time.Time) ([]domain.Metrics, error) {
	return nil, nil
}
func (t *recordingTSDB) QueryAll(context.Context, time.Time, time.Time) (map[string][]domain.Metrics, error) {
	return map[string][]domain.Metrics{}, nil
}
func (t *recordingTSDB) AppendNetworkProbes(_ context.Context, nodeID string, points []domain.NetworkProbePoint) error {
	t.networkProbeNodeID = nodeID
	t.networkProbes = append(t.networkProbes, points...)
	return nil
}
func (t *recordingTSDB) QueryNetworkProbes(context.Context, string, time.Time, time.Time) ([]domain.NetworkProbePoint, error) {
	return t.networkProbes, nil
}
func (t *recordingTSDB) AppendTaskMetrics(_ context.Context, nodeID, measurement, taskID, taskName string, timestamp time.Time, metrics map[string]float64) error {
	t.taskMetricNodeID = nodeID
	t.taskMetricMeasurement = measurement
	t.taskMetricTaskID = taskID
	t.taskMetricTaskName = taskName
	t.taskMetricTimestamp = timestamp
	t.taskMetrics = metrics
	return nil
}
func (t *recordingTSDB) DeleteNode(context.Context, string) error { return nil }
func (t *recordingTSDB) Stats(context.Context) (tsdb.Stats, error) {
	return tsdb.Stats{Driver: "test", Ready: true}, nil
}
func (t *recordingTSDB) Close(context.Context) error { return nil }
