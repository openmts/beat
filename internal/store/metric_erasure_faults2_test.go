package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func erasureFleetRequest(id string) model.MetricErasureCreateRequest {
	scope := model.MetricErasureFleetTelemetry
	return model.MetricErasureCreateRequest{Scope: scope, RequestID: id,
		Confirmation: model.MetricErasureConfirmationFor(scope)}
}

// TestMetricErasureCreateRejectsInvalidAndConflictingRequests 覆盖擦除任务的
// 入参门禁、重放识别与「同请求号不同参数」冲突。
func TestMetricErasureCreateRejectsInvalidAndConflictingRequests(t *testing.T) {
	fixture := newErasureRestoreFixture(t)
	ctx := t.Context()
	requestID := uuid.NewString()
	create := MetricErasureCreate{Request: erasureFleetRequest(requestID), Now: fixture.now}
	if _, replay, err := fixture.store.Create(ctx, create); err != nil || replay {
		t.Fatalf("首次创建 = %v/%v", replay, err)
	}
	if _, replay, err := fixture.store.Create(ctx, create); err != nil || !replay {
		t.Fatalf("重放同一请求 = %v/%v", replay, err)
	}
	historyScope := model.MetricErasureAllMetricHistory
	conflict := MetricErasureCreate{Request: model.MetricErasureCreateRequest{Scope: historyScope,
		RequestID: requestID, Confirmation: model.MetricErasureConfirmationFor(historyScope)}, Now: fixture.now}
	if _, _, err := fixture.store.Create(ctx, conflict); !errors.Is(err, model.ErrMetricErasureConflict) {
		t.Fatalf("同请求号不同参数 = %v", err)
	}
	invalid := MetricErasureCreate{Request: model.MetricErasureCreateRequest{RequestID: "not-a-uuid"}, Now: fixture.now}
	if _, _, err := fixture.store.Create(ctx, invalid); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("非法作用域 = %v", err)
	}
	closed := NewMetricErasureStore(closedSQLiteStore(t).DB)
	if _, _, err := closed.Create(ctx, create); err == nil {
		t.Fatal("关闭的数据库接受了擦除任务")
	}
}

// TestMetricErasureCreateFailsClosedWhenQueueWriteIsBlocked 确认任务表写入失败时
// 事务整体回滚，不留下没有队列行的墓碑。
func TestMetricErasureCreateFailsClosedWhenQueueWriteIsBlocked(t *testing.T) {
	fixture := newErasureRestoreFixture(t)
	ctx := t.Context()
	if _, err := fixture.store.db.ExecContext(ctx, `CREATE TRIGGER cov_block_erasure BEFORE INSERT ON metric_erasure_jobs
		BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	blocked := MetricErasureCreate{Request: erasureFleetRequest(uuid.NewString()), Now: fixture.now}
	if _, _, err := fixture.store.Create(ctx, blocked); err == nil {
		t.Fatal("被阻断的擦除任务写入了队列")
	}
	var count int
	if err := fixture.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metric_erasure_jobs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("失败创建留下 %d 条擦除任务", count)
	}
}

// TestMetricErasureLifecycleRejectsIllegalTransitions 覆盖删除阶段状态机：
// 只有从当前阶段出发的跃迁才被接受。
func TestMetricErasureLifecycleRejectsIllegalTransitions(t *testing.T) {
	fixture := newErasureRestoreFixture(t)
	ctx := t.Context()
	jobID := uuid.NewString()
	seedRestoreJob(t, fixture, jobID, model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "",
		model.MetricErasureQueued, "", 0)
	if _, err := fixture.store.Claim(ctx, "", fixture.now); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("空 worker 认领 = %v", err)
	}
	claim, err := fixture.store.Claim(ctx, "worker-a", fixture.now)
	if err != nil || claim == nil {
		t.Fatalf("认领 = %+v/%v", claim, err)
	}
	if err := fixture.store.BeginDeleting(ctx, *claim, "short", 0, fixture.now); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("非法摘要 = %v", err)
	}
	if err := fixture.store.Advance(ctx, claim.Job.ID, "cpu", fixture.now); err == nil {
		t.Fatal("认领阶段推进游标被接受")
	}
	if err := fixture.store.BeginCompacting(ctx, claim.Job.ID, fixture.now); err == nil {
		t.Fatal("认领阶段进入压缩被接受")
	}
	if err := fixture.store.Complete(ctx, claim.Job.ID, fixture.now); err == nil {
		t.Fatal("认领阶段标记成功被接受")
	}
}

// TestMetricErasureLifecycleFailsClosedOnLeaseFaults 确认删除阶段的游标推进、
// 失败结算与终态拒绝都按状态机执行。
func TestMetricErasureLifecycleFailsClosedOnLeaseFaults(t *testing.T) {
	fixture := newErasureRestoreFixture(t)
	ctx := t.Context()
	jobID := uuid.NewString()
	seedRestoreJob(t, fixture, jobID, model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "",
		model.MetricErasureQueued, "", 0)
	claim, err := fixture.store.Claim(ctx, "worker-a", fixture.now)
	if err != nil || claim == nil {
		t.Fatalf("认领 = %+v/%v", claim, err)
	}
	digest := strings.Repeat("a", model.MaximumMetricErasureDigestBytes)
	if err := fixture.store.BeginDeleting(ctx, *claim, digest, fixture.now.UnixNano(), fixture.now); err != nil {
		t.Fatalf("进入删除阶段 = %v", err)
	}
	if err := fixture.store.Advance(ctx, claim.Job.ID, "cpu", fixture.now); err != nil {
		t.Fatalf("推进游标 = %v", err)
	}
	if err := fixture.store.Fail(ctx, claim.Job.ID, strings.Repeat("x", model.MaximumMetricErasureErrorBytes+1), fixture.now); err != nil {
		t.Fatalf("失败结算 = %v", err)
	}
	var code string
	if err := fixture.store.db.QueryRowContext(ctx,
		"SELECT last_error_code FROM metric_erasure_jobs WHERE id=?", jobID).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != model.MetricErasureErrorInternal {
		t.Fatalf("超长错误码 = %q", code)
	}
	if err := fixture.store.Release(ctx, jobID, fixture.now); err == nil {
		t.Fatal("终态任务被交回队列")
	}
	if _, err := fixture.store.db.ExecContext(ctx, `CREATE TRIGGER cov_block_erasure_update BEFORE UPDATE ON metric_erasure_jobs
		BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	seedRestoreJob(t, fixture, uuid.NewString(), model.MetricErasureFleetTelemetry, model.MetricErasureTargetNone, "",
		model.MetricErasureQueued, "", 0)
	if _, err := fixture.store.Claim(ctx, "worker-b", fixture.now); err == nil {
		t.Fatal("被阻断的认领写入了租约")
	}
}
