// v219：擦除编排在账本拒绝、上下文关闭与运行期 panic 时的失败关闭覆盖。
//
// 账本故障用 SQLite 触发器注入：它让指定的状态迁移直接报错，不需要自定义驱动，
// 也不会碰 MTS 目录本身。
package metricserasure

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type abortTrigger struct{ name, condition string }

func installAbortTrigger(t *testing.T, fixture erasureFixture, trigger abortTrigger) {
	t.Helper()
	statement := "CREATE TRIGGER " + trigger.name + " BEFORE UPDATE ON metric_erasure_jobs WHEN " +
		trigger.condition + " BEGIN SELECT RAISE(ABORT,'injected ledger failure'); END"
	if _, err := fixture.sqlite.DB.ExecContext(t.Context(), statement); err != nil {
		t.Fatalf("install abort trigger: %v", err)
	}
}

func claimOnlyJob(t *testing.T, fixture erasureFixture) store.MetricErasureClaim {
	t.Helper()
	claim, err := fixture.jobs.Claim(t.Context(), "worker-1", model.NowUTC())
	if err != nil || claim == nil {
		t.Fatalf("claim erasure job = %#v %v", claim, err)
	}
	return *claim
}

func requireJobState(t *testing.T, fixture erasureFixture, id, state, code string) {
	t.Helper()
	job, err := fixture.jobs.Get(t.Context(), id)
	if err != nil || job == nil {
		t.Fatalf("read erasure job = %#v %v", job, err)
	}
	if job.State != state || job.LastErrorCode != code {
		t.Fatalf("job = %s/%s, want %s/%s", job.State, job.LastErrorCode, state, code)
	}
}

// 账本拒绝「开始删除」时必须原地失败：不能带着未冻结的清单继续擦除。
func TestEraseStopsWhenLedgerRejectsDeleting(t *testing.T) {
	fixture := newErasureFixture(t)
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	claim := claimOnlyJob(t, fixture)
	installAbortTrigger(t, fixture, abortTrigger{name: "abort_deleting", condition: "NEW.state='deleting'"})
	err := fixture.service.erase(t.Context(), claim)
	if err == nil || jobFailure(err) {
		t.Fatalf("erase error = %v", err)
	}
	requireJobState(t, fixture, job.ID, model.MetricErasureClaimed, "")
}

// 任务级失败之外的错误必须终止 worker：只有写入码的失败才允许继续处理后续任务。
func TestRunStopsOnUncompensatedEraseFailure(t *testing.T) {
	fixture := newErasureFixture(t)
	fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	installAbortTrigger(t, fixture, abortTrigger{name: "abort_deleting", condition: "NEW.state='deleting'"})
	err := fixture.service.Run(t.Context())
	if err == nil || jobFailure(err) {
		t.Fatalf("run error = %v", err)
	}
}

// 关闭时的领取失败不是故障：上下文已取消就必须干净退出，避免监督器误报降级。
func TestRunReturnsCleanlyWhenShutdownCancelsClaim(t *testing.T) {
	fixture := newErasureFixture(t)
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.service.Run(ctx); err != nil {
		t.Fatalf("shutdown run = %v", err)
	}
}

// 运行期 panic（例如 MTS 运行时未接线）必须转成脱敏失败码，而不是让进程崩溃。
func TestProcessRecoversFromUnwiredRuntimePanic(t *testing.T) {
	fixture := newErasureFixture(t)
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	claim := claimOnlyJob(t, fixture)
	unwired := NewService(fixture.jobs, nil)
	err := unwired.Process(t.Context(), claim)
	if erasureErrorCode(err) != model.MetricErasureErrorInternal || !jobFailure(err) {
		t.Fatalf("panic recovery error = %v", err)
	}
	requireJobState(t, fixture, job.ID, model.MetricErasureFailed, model.MetricErasureErrorInternal)
}

// 删除阶段必须尊重取消与已关闭的会话：两者都让游标停在原地，稍后可重试。
func TestDeleteMeasurementsStopsOnShutdownAndClosedSession(t *testing.T) {
	fixture := newErasureFixture(t)
	at := model.NowUTC().Add(-time.Minute)
	if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "cpu", 42, at); err != nil {
		t.Fatal(err)
	}
	job := fixture.create(t, model.MetricErasureAllMetricHistory, "", model.MetricErasureAllConfirmation)
	session, err := fixture.mts.BeginMetricErasure(t.Context(), job.Scope, "", "")
	if err != nil {
		t.Fatal(err)
	}
	plan := session.Plan()
	if len(plan.Measurements) == 0 {
		t.Fatal("frozen plan is empty")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.service.deleteMeasurements(ctx, session, job, plan); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown deletion error = %v", err)
	}
	session.Close()
	if err := fixture.service.deleteMeasurements(t.Context(), session, job, plan); !errors.Is(err, store.ErrMTSStoreClosed) {
		t.Fatalf("closed session deletion error = %v", err)
	}
}

// 压缩与收尾阶段都必须失败关闭：账本拒绝迁移时任务保持可重试，不能假装成功。
func TestEraseStopsWhenLedgerRejectsLaterPhases(t *testing.T) {
	for _, phase := range []struct{ name, state, stopped string }{
		{"compacting", "compacting", model.MetricErasureDeleting},
		{"finalizing", "finalizing", model.MetricErasureCompacting},
	} {
		t.Run(phase.name, func(t *testing.T) {
			fixture := newErasureFixture(t)
			at := model.NowUTC().Add(-time.Minute)
			if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "cpu", 7, at); err != nil {
				t.Fatal(err)
			}
			job := fixture.create(t, model.MetricErasureAllMetricHistory, "", model.MetricErasureAllConfirmation)
			claim := claimOnlyJob(t, fixture)
			installAbortTrigger(t, fixture, abortTrigger{name: "abort_" + phase.name,
				condition: "NEW.state='" + phase.state + "'"})
			err := fixture.service.erase(t.Context(), claim)
			if err == nil || !strings.Contains(err.Error(), "injected ledger failure") {
				t.Fatalf("erase error = %v", err)
			}
			requireJobState(t, fixture, job.ID, phase.stopped, "")
		})
	}
}

// 实体删除失败必须留下 finalize 失败码；节点行此时仍然在库，运维可以重试。
func TestEraseFailsClosedWhenEntityDeletionFails(t *testing.T) {
	fixture := newErasureFixture(t)
	at := model.NowUTC().Add(-time.Minute)
	if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "cpu", 7, at); err != nil {
		t.Fatal(err)
	}
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	claim := claimOnlyJob(t, fixture)
	statement := "CREATE TRIGGER abort_node_delete BEFORE DELETE ON nodes BEGIN SELECT RAISE(ABORT,'injected entity failure'); END"
	if _, err := fixture.sqlite.DB.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
	err := fixture.service.erase(t.Context(), claim)
	if erasureErrorCode(err) != model.MetricErasureErrorFinalize {
		t.Fatalf("finalize error = %v", err)
	}
	requireJobState(t, fixture, job.ID, model.MetricErasureFailed, model.MetricErasureErrorFinalize)
}

// flippingContext 让 Err() 在观察者回调里才转为已取消：用来验证「关闭期间的任务失败
// 归一成干净退出」这条分支（Run 在报告失败之后才读 ctx.Err()）。
type flippingContext struct {
	context.Context
	flipped atomic.Bool
}

func (ctx *flippingContext) Done() <-chan struct{} { return nil }

func (ctx *flippingContext) Err() error {
	if ctx.flipped.Load() {
		return context.Canceled
	}
	return nil
}

func TestRunTreatsTaskFailureDuringShutdownAsClean(t *testing.T) {
	fixture := newErasureFixture(t)
	fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	installAbortTrigger(t, fixture, abortTrigger{name: "abort_deleting", condition: "NEW.state='deleting'"})
	ctx := &flippingContext{Context: context.Background()}
	fixture.service.SetObserver(func(error) { ctx.flipped.Store(true) })
	if err := fixture.service.Run(ctx); err != nil {
		t.Fatalf("shutdown run = %v", err)
	}
}

func TestServiceSignalCoalescesWhenBufferIsFull(t *testing.T) {
	fixture := newErasureFixture(t)
	fixture.service.Signal()
	fixture.service.Signal()
	if !fixture.service.idle(t.Context()) {
		t.Fatal("coalesced signal was lost")
	}
}
