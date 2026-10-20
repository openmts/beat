package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestMetricErasureInvalidIdentifiersAndLimits(t *testing.T) {
	jobs := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)

	// 形状非法的任务号不会命中查询，读取直接返回空而不是错误。
	if job, err := jobs.Get(t.Context(), "not-a-uuid"); err != nil || job != nil {
		t.Fatalf("invalid get = %+v, err = %v", job, err)
	}
	if err := jobs.Cancel(t.Context(), "not-a-uuid", now); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("invalid cancel error = %v", err)
	}
	if err := jobs.Cancel(t.Context(), "11111111-1111-4111-8111-111111111111", time.Time{}); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("zero-clock cancel error = %v", err)
	}
	if err := jobs.Retry(t.Context(), "not-a-uuid", now); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("invalid retry error = %v", err)
	}
	if err := jobs.Retry(t.Context(), "11111111-1111-4111-8111-111111111111", time.Time{}); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("zero-clock retry error = %v", err)
	}
	// 未知任务号在取消与重试上都是 404 语义。
	unknown := "11111111-1111-4111-8111-111111111111"
	if err := jobs.Cancel(t.Context(), unknown, now); !errors.Is(err, model.ErrMetricErasureMissing) {
		t.Fatalf("missing cancel error = %v", err)
	}
	if err := jobs.Retry(t.Context(), unknown, now); !errors.Is(err, model.ErrMetricErasureMissing) {
		t.Fatalf("missing retry error = %v", err)
	}

	// Create 的前置校验在事务外短路。
	for name, input := range map[string]MetricErasureCreate{
		"zero clock":  {Request: metricErasureRequest(unknown, model.MetricErasureNodeHistory, erasureNodeID, "server-one")},
		"bad scope":   {Request: metricErasureRequest(unknown, "unknown", "", ""), Now: now},
		"bad request": {Request: metricErasureRequest("bad", model.MetricErasureFleetTelemetry, "", model.MetricErasureFleetConfirmation), Now: now},
	} {
		if _, _, err := jobs.Create(t.Context(), input); !errors.Is(err, model.ErrMetricErasureInvalid) {
			t.Fatalf("%s create error = %v", name, err)
		}
	}

	// 列表上限被夹到默认值，避免调用方用巨大 limit 拖垮存储。
	if page, err := jobs.List(t.Context(), 0); err != nil || len(page) != 0 {
		t.Fatalf("default list = %+v, err = %v", page, err)
	}
	if page, err := jobs.List(t.Context(), MaximumMetricErasureRows+1); err != nil || len(page) != 0 {
		t.Fatalf("clamped list = %+v, err = %v", page, err)
	}
}

func TestMetricErasureStatusCountsAndActiveScopes(t *testing.T) {
	jobs := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	first := createMetricErasureJob(t, jobs, "88888888-8888-4888-8888-888888888888",
		model.MetricErasureNodeHistory, erasureNodeID, "server-one", now)
	createMetricErasureJob(t, jobs, "99999999-9999-4999-8999-999999999999",
		model.MetricErasureNetworkTaskHistory, erasureTaskID, "Probe", now)
	if _, err := jobs.db.ExecContext(t.Context(),
		"UPDATE metric_erasure_jobs SET state=? WHERE id=?", model.MetricErasureSucceeded, first.ID); err != nil {
		t.Fatalf("mark job succeeded: %v", err)
	}
	status, err := jobs.Status(t.Context())
	if err != nil {
		t.Fatalf("status = %v", err)
	}
	counts := status.Counts()
	if counts[model.MetricErasureSucceeded] != 1 || counts[model.MetricErasureQueued] != 1 {
		t.Fatalf("status counts = %+v", counts)
	}
	// 只有活动状态计入活动范围；已完成的节点擦除不再占用实体。
	active := map[string]int{}
	for _, scope := range MetricErasureActiveScopes(status) {
		active[scope.Name] = scope.Count
	}
	if active[model.MetricErasureNetworkTaskHistory] != 1 || active[model.MetricErasureNodeHistory] != 0 {
		t.Fatalf("active scopes = %+v", active)
	}
	if !status.Active() || status.Degraded() {
		t.Fatalf("status flags = active:%v degraded:%v", status.Active(), status.Degraded())
	}
	if states := MetricErasureStates(); len(states) != 8 {
		t.Fatalf("states = %+v", states)
	}
	if activeMetricErasureState(model.MetricErasureSucceeded) {
		t.Fatal("terminal state counted as active")
	}
}

func TestMetricErasureFinalizeEntityRemovesApplicationRows(t *testing.T) {
	jobs := metricErasureFixture(t)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	// 纯历史擦除使用独立节点：同一实体的并发擦除会被并发闸门拒绝。
	const spareNodeID = "15151515-1515-4515-8515-151515151515"
	if _, err := jobs.db.ExecContext(t.Context(),
		"INSERT INTO nodes (id, name, host, port, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		spareNodeID, "server-two", "192.0.2.2", 22, now, now); err != nil {
		t.Fatalf("insert spare node: %v", err)
	}
	history := createMetricErasureJob(t, jobs, "14141414-1414-4414-8414-141414141414",
		model.MetricErasureNodeHistory, spareNodeID, "server-two", now)
	node := createMetricErasureJob(t, jobs, "12121212-1212-4212-8212-121212121212",
		model.MetricErasureDeleteNode, erasureNodeID, "server-one", now)
	task := createMetricErasureJob(t, jobs, "13131313-1313-4313-8313-131313131313",
		model.MetricErasureDeleteNetworkTask, erasureTaskID, "Probe", now)

	// 未产生实体墓碑的范围是空操作，零时钟同样不应该触碰数据库。
	if err := jobs.FinalizeEntity(t.Context(), history, now); err != nil {
		t.Fatalf("non entity finalize = %v", err)
	}
	if err := jobs.FinalizeEntity(t.Context(), node, time.Time{}); err != nil {
		t.Fatalf("zero-clock finalize = %v", err)
	}
	for name, job := range map[string]model.MetricErasureJob{"node": node, "task": task} {
		if err := jobs.FinalizeEntity(t.Context(), job, now.Add(time.Minute)); err != nil {
			t.Fatalf("%s finalize = %v", name, err)
		}
		// 重复收尾是幂等的：第二次删除匹配 0 行且不报错。
		if err := jobs.FinalizeEntity(t.Context(), job, now.Add(2*time.Minute)); err != nil {
			t.Fatalf("%s repeat finalize = %v", name, err)
		}
	}
	var nodes, tasks int
	if err := jobs.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM nodes WHERE id = ?", erasureNodeID).Scan(&nodes); err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if err := jobs.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM network_tasks WHERE id = ?", erasureTaskID).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if nodes != 0 || tasks != 0 {
		t.Fatalf("finalized rows = %d/%d", nodes, tasks)
	}
}
