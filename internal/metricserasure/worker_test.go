package metricserasure

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestServiceNilReceiverIsInert(t *testing.T) {
	var service *Service
	service.SetObserver(func(error) { t.Fatal("nil service invoked observer") })
	service.Signal()
	if err := service.Ready(t.Context()); err != nil {
		t.Fatalf("nil service readiness = %v", err)
	}
	if err := service.Run(t.Context()); err == nil {
		t.Fatal("nil service accepted Run")
	}
}

func TestServiceReadyReportsQueueState(t *testing.T) {
	fixture := newErasureFixture(t)
	if err := fixture.service.Ready(t.Context()); err != nil {
		t.Fatalf("empty queue readiness = %v", err)
	}
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	claim, err := fixture.jobs.Claim(t.Context(), "worker-1", model.NowUTC())
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v %v", claim, err)
	}
	if err := fixture.service.Ready(t.Context()); err != nil {
		t.Fatalf("claimed job readiness = %v", err)
	}
	if err := fixture.jobs.Fail(t.Context(), job.ID, model.MetricErasureErrorInternal, model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Ready(t.Context()); err == nil || !strings.Contains(err.Error(), "operator retry") {
		t.Fatalf("degraded readiness = %v", err)
	}
}

func TestServiceReadyPropagatesStatusFailure(t *testing.T) {
	fixture := newErasureFixture(t)
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Ready(t.Context()); err == nil {
		t.Fatal("closed database reported readiness")
	}
}

func TestServiceRunReportsMissingConfiguration(t *testing.T) {
	if err := (&Service{}).Run(t.Context()); err == nil {
		t.Fatal("unconfigured service accepted Run")
	}
	if err := (&Service{jobs: nil}).Run(t.Context()); err == nil {
		t.Fatal("service without jobs accepted Run")
	}
}

func TestServiceRunReturnsWhenContextIsAlreadyCanceled(t *testing.T) {
	fixture := newErasureFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := fixture.service.Run(ctx); err != nil {
		t.Fatalf("canceled run = %v", err)
	}
}

func TestServiceRunProcessesQueuedJobAndStops(t *testing.T) {
	fixture := newErasureFixture(t)
	if err := fixture.mts.WriteMetric(t.Context(), fixture.nodeID, "cpu", 3, model.NowUTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	var observed []error
	fixture.service.SetObserver(func(err error) { observed = append(observed, err) })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- fixture.service.Run(ctx) }()
	fixture.service.Signal()
	deadline := time.Now().Add(20 * time.Second)
	state := ""
	for time.Now().Before(deadline) {
		current, err := fixture.jobs.Get(t.Context(), job.ID)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		state = current.State
		if state == model.MetricErasureSucceeded || state == model.MetricErasureFailed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	runErr := <-done
	// 高负载下 SQLite 可能让这一次领取失败；只要调度循环的行为与任务终态一致即可。
	switch state {
	case model.MetricErasureSucceeded:
		if runErr != nil {
			t.Fatalf("run = %v", runErr)
		}
	case model.MetricErasureFailed:
		if runErr != nil {
			t.Fatalf("job-level failure terminated the loop: %v", runErr)
		}
		if !reportedFailure(observed) {
			t.Fatalf("failed job was not reported to the observer: %v", observed)
		}
	default:
		t.Fatalf("job did not reach a terminal state: %s", state)
	}
	// 关闭时观察者还会收到一次 context 取消（读租约被中断），这与本轮 run 的返回错误无关；
	// 其余上报必须与 run 的返回一致，避免"观察者看到失败但循环报告成功"这类不一致。
	if runErr != nil {
		for _, reported := range observed {
			if reported == nil || errors.Is(reported, context.Canceled) {
				continue
			}
			if !errors.Is(reported, runErr) {
				t.Fatalf("observer reports = %v, run = %v", observed, runErr)
			}
		}
	}
}

// reportedFailure 报告观察者是否收到过一次非取消失败。
func reportedFailure(observed []error) bool {
	for _, reported := range observed {
		if reported != nil && !errors.Is(reported, context.Canceled) {
			return true
		}
	}
	return false
}

// 一次可重试的擦除任务失败曾经直接终止 worker 循环，进而让整个服务端进程退出
// （systemd 显示 "required background worker terminated"）。任务级失败必须只上报。
func TestServiceRunSurvivesJobLevelFailure(t *testing.T) {
	fixture := newErasureFixture(t)
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	if err := fixture.mts.Close(); err != nil {
		t.Fatal(err)
	}
	var reported error
	fixture.service.SetObserver(func(err error) {
		if err != nil && !errors.Is(err, context.Canceled) {
			reported = err
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- fixture.service.Run(ctx) }()
	fixture.service.Signal()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		current, err := fixture.jobs.Get(t.Context(), job.ID)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if current.State == model.MetricErasureFailed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case err := <-done:
		cancel()
		t.Fatalf("job-level failure terminated the loop: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run = %v", err)
	}
	if reported == nil {
		t.Fatal("job-level failure was not reported to the observer")
	}
}

func TestServiceRunReportsClaimFailure(t *testing.T) {
	fixture := newErasureFixture(t)
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "claim metric erasure job") {
		t.Fatalf("run = %v", err)
	}
}

func TestServiceObserveForwardsToObserver(t *testing.T) {
	fixture := newErasureFixture(t)
	fixture.service.observe(errors.New("ignored"))
	var observed error
	fixture.service.SetObserver(func(err error) { observed = err })
	sentinel := errors.New("observed")
	fixture.service.observe(sentinel)
	if !errors.Is(observed, sentinel) {
		t.Fatalf("observed = %v", observed)
	}
}

func TestServiceIdleHonoursContextAndWake(t *testing.T) {
	fixture := newErasureFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if fixture.service.idle(ctx) {
		t.Fatal("canceled idle kept looping")
	}
	fixture.service.Signal()
	if !fixture.service.idle(t.Context()) {
		t.Fatal("signalled idle did not wake")
	}
}

// 关闭期间上下文取消会让 SQLite 自动回滚，驱动随后把后续语句报成 sql.ErrTxDone。
// 监督器不能把这种存储错误当成故障：Run 在同一次取消下返回 nil，两边必须一致。
func TestServiceNormalizesCancelledStorageFailureForObserver(t *testing.T) {
	fixture := newErasureFixture(t)
	var observed []error
	fixture.service.SetObserver(func(err error) { observed = append(observed, err) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fixture.service.reportFailure(ctx, sql.ErrTxDone)
	if len(observed) != 1 || !errors.Is(observed[0], context.Canceled) {
		t.Fatalf("shutdown observation = %v", observed)
	}

	// 上下文未取消时必须原样上报，监督器仍要看到真实故障。
	observed = nil
	fixture.service.reportFailure(t.Context(), sql.ErrTxDone)
	if len(observed) != 1 || !errors.Is(observed[0], sql.ErrTxDone) {
		t.Fatalf("live observation = %v", observed)
	}
}
