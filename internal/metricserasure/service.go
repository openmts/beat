// Package metricserasure 运行耐久擦除任务：领取租约、发布 MTS tombstone、压缩并收尾。
//
// 该 package 只做编排：状态与游标由 store 负责，tombstone 由 MTS 负责，HTTP 层只创建
// 任务。任何失败都写入脱敏错误码并保持任务可见、可重试。
package metricserasure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// Interval 是空闲轮询间隔；创建任务后调用方可用 Signal 立即唤醒 worker。
const Interval = 5 * time.Second

type Service struct {
	jobs     *store.MetricErasureStore
	mts      *store.MTSStore
	now      func() time.Time
	workerID string
	observer func(error)
	wake     chan struct{}
}

func NewService(jobs *store.MetricErasureStore, mts *store.MTSStore) *Service {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "beat"
	}
	return &Service{jobs: jobs, mts: mts, now: model.NowUTC,
		workerID: host + ":" + uuid.NewString()[:8], wake: make(chan struct{}, 1)}
}

// SetObserver 注册监督钩子；监督器用它区分健康、失败与重启。
func (service *Service) SetObserver(observer func(error)) {
	if service != nil {
		service.observer = observer
	}
}

// Signal 唤醒空闲 worker；缓冲区为 1，重复唤醒会被合并。
func (service *Service) Signal() {
	if service == nil {
		return
	}
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

// Ready 报告耐久队列状态：活动任务仍算就绪，只有耗尽重试的失败任务降级。
func (service *Service) Ready(ctx context.Context) error {
	if service == nil || service.jobs == nil {
		return nil
	}
	status, err := service.jobs.Status(ctx)
	if err != nil {
		return err
	}
	if status.Degraded() {
		return errors.New("metric erasure jobs failed and need an operator retry")
	}
	return nil
}

// Run 阻塞持有 worker 循环，直到上下文取消。
func (service *Service) Run(ctx context.Context) error {
	if service == nil || service.jobs == nil || service.mts == nil {
		return errors.New("metric erasure service is not configured")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		claim, err := service.jobs.Claim(ctx, service.workerID, service.now())
		if err != nil {
			service.reportFailure(ctx, err)
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("claim metric erasure job: %w", err)
		}
		if claim == nil {
			// 空闲迭代同样上报成功：监督器只用 last_success 判断健康，
			// 只在出错时上报会让就绪探针在启动宽限期结束后永久降级。
			service.observe(nil)
			if !service.idle(ctx) {
				return nil
			}
			continue
		}
		if err := service.Process(ctx, *claim); err != nil {
			service.reportFailure(ctx, err)
			if ctx.Err() != nil {
				return nil
			}
			// 任务级失败已经写入脱敏错误码，任务保持可见可重试；它不能终止 worker，
			// 否则一次可重试的擦除任务就会拖垮整个服务端进程。
			if jobFailure(err) {
				continue
			}
			return err
		}
		service.observe(nil)
	}
}

func (service *Service) idle(ctx context.Context) bool {
	timer := time.NewTimer(Interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-service.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (service *Service) observe(err error) {
	if service.observer != nil {
		service.observer(err)
	}
}

// reportFailure 上报一次领取或执行失败。关闭期间上下文取消会让 SQLite 自动回滚，驱动随后把
// 后续语句报成 sql.ErrTxDone 一类存储错误；这类错误不是健康故障，必须归一成取消后上报，
// 否则监督器会看到「观察者报失败、循环报成功」的不一致，重启窗口内还会误报降级。
func (service *Service) reportFailure(ctx context.Context, err error) {
	if ctx.Err() != nil {
		service.observe(ctx.Err())
		return
	}
	service.observe(err)
}

// Process 执行一次已领取的任务；它永不 panic，也永不静默丢弃失败任务。
func (service *Service) Process(ctx context.Context, claim store.MetricErasureClaim) (resultErr error) {
	job := claim.Job
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.ErrorContext(ctx, "metric erasure worker panicked", "job_id", job.ID, "scope", job.Scope, "phase", "worker")
			resultErr = service.fail(ctx, job, model.MetricErasureErrorInternal)
		}
	}()
	if err := service.erase(ctx, claim); err != nil {
		slog.ErrorContext(ctx, "metric erasure job failed", "job_id", claim.Job.ID, "scope", claim.Job.Scope,
			"attempt", claim.Job.AttemptCount, "error_code", erasureErrorCode(err))
		return err
	}
	return nil
}

// jobFailure 报告错误是否只是任务级失败：失败码已落库，循环应当继续处理后续任务。
func jobFailure(err error) bool {
	var coded *codedError
	return errors.As(err, &coded)
}

func erasureErrorCode(err error) string {
	var coded *codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return model.MetricErasureErrorInternal
}

type codedError struct {
	code string
	err  error
}

func (failure codedError) Error() string { return failure.err.Error() }

func (failure codedError) Unwrap() error { return failure.err }
