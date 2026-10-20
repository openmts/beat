package applicationgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrAlreadyRunning 表示同一时刻只允许一次应用历史清理。
var ErrAlreadyRunning = errors.New("application history cleanup is already running")

const (
	defaultSchedulerInterval = 15 * time.Minute
	maximumRunDuration       = 10 * time.Minute
	batchYield               = 25 * time.Millisecond
)

type retentionStore interface {
	Get(context.Context) (model.ApplicationRetentionSettings, model.ApplicationRetentionStatus, error)
	Update(context.Context, model.ApplicationRetentionSettings) (model.ApplicationRetentionSettings, error)
	MarkStarted(context.Context, string, time.Time) error
	MarkCompleted(context.Context, time.Time, time.Duration, string, string, model.ApplicationRetentionCounts) error
	DeleteResolvedAlertEventsBefore(context.Context, time.Time, int) (int64, error)
	DeleteAuditEventsBefore(context.Context, time.Time, int) (int64, error)
	DeleteRevokedSessionsBefore(context.Context, time.Time, time.Time, int) (int64, error)
}

// Service 独立于 MTS 保留策略清理应用历史；禁用 MTS 年龄删除不影响这里的卫生清理。
type Service struct {
	ctx      context.Context
	store    retentionStore
	now      func() time.Time
	observer func(error)
	mu       sync.Mutex
	wg       sync.WaitGroup
	live     bool
}

func New(ctx context.Context, store retentionStore) *Service {
	return &Service{ctx: ctx, store: store, now: func() time.Time { return time.Now().UTC() }}
}

// WithClock 覆盖当前时间，供测试注入确定时钟。
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// SetObserver 注册监督钩子；监督器用它区分健康、失败与重启。
func (s *Service) SetObserver(observer func(error)) {
	if s != nil {
		s.observer = observer
	}
}

func (s *Service) observe(err error) {
	if s.observer != nil {
		s.observer(err)
	}
}

func (s *Service) Overview(ctx context.Context) (model.ApplicationRetentionOverview, error) {
	settings, status, err := s.store.Get(ctx)
	if err != nil {
		return model.ApplicationRetentionOverview{}, err
	}
	return model.ApplicationRetentionOverview{
		Settings: settings, Status: status,
		SessionRetentionDays: model.ApplicationSessionRetentionDays,
	}, nil
}

func (s *Service) UpdateSettings(
	ctx context.Context,
	settings model.ApplicationRetentionSettings,
) (model.ApplicationRetentionSettings, error) {
	return s.store.Update(ctx, settings)
}

// TriggerManual 在后台发起一次清理；重复触发返回 ErrAlreadyRunning。
func (s *Service) TriggerManual() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live {
		return ErrAlreadyRunning
	}
	s.live = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.finish()
		// 手动清理的失败已在 execute 内记录；这里不回报 worker 健康，避免把
		// 一次手动触发失败误判成调度器本身不健康。
		_ = s.execute(model.ApplicationRetentionTriggerManual)
	}()
	return nil
}

func (s *Service) finish() {
	s.mu.Lock()
	s.live = false
	s.mu.Unlock()
}

// Run 是受监督的调度循环：到期时同步执行一次清理，取消时立即退出。
func (s *Service) Run(ctx context.Context) {
	s.observe(s.runDue(ctx))
	ticker := time.NewTicker(defaultSchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.observe(s.runDue(ctx))
		}
	}
}

func (s *Service) runDue(ctx context.Context) error {
	overview, err := s.Overview(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "application history cleanup overview failed", "error_code", "overview")
		return err
	}
	if !automaticCleanupDue(overview, s.now()) {
		// 空闲迭代同样上报成功：监督器只看 last_success，只在出错时上报
		// 会让就绪探针在启动宽限期结束后永久停留在 starting。
		return nil
	}
	s.mu.Lock()
	if s.live {
		s.mu.Unlock()
		return nil
	}
	s.live = true
	s.mu.Unlock()
	defer s.finish()
	return s.execute(model.ApplicationRetentionTriggerAutomatic)
}

func (s *Service) execute(trigger string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), maximumRunDuration)
	defer cancel()
	startedAt := s.now()
	if err := s.store.MarkStarted(ctx, trigger, startedAt); err != nil {
		slog.ErrorContext(ctx, "application history cleanup start failed", "error_code", "start")
		return err
	}
	counts, runErr := s.pass(ctx, startedAt)
	completedAt := s.now()
	result, code := model.ApplicationRetentionStatusSuccess, ""
	if runErr != nil {
		result, code = model.ApplicationRetentionStatusFailed, errorCode(runErr)
		slog.ErrorContext(ctx, "application history cleanup failed", "error_code", code)
	}
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer persistCancel()
	if err := s.store.MarkCompleted(persistCtx, completedAt, completedAt.Sub(startedAt), result, code, counts); err != nil {
		slog.ErrorContext(persistCtx, "application history cleanup completion failed", "error_code", "complete")
		return err
	}
	return runErr
}

// pass 按规定顺序清理：会话卫生、已解决告警、管理员审计。
func (s *Service) pass(ctx context.Context, now time.Time) (model.ApplicationRetentionCounts, error) {
	settings, _, err := s.store.Get(ctx)
	if err != nil {
		return model.ApplicationRetentionCounts{}, err
	}
	var counts model.ApplicationRetentionCounts
	var failures error
	if counts.Sessions, err = s.deleteAll(ctx, func(ctx context.Context) (int64, error) {
		return s.store.DeleteRevokedSessionsBefore(ctx, now,
			now.AddDate(0, 0, -model.ApplicationSessionRetentionDays), model.ApplicationRetentionBatchSize)
	}); err != nil {
		failures = errors.Join(failures, fmt.Errorf("sessions: %w", err))
	}
	if counts.AlertEvents, err = s.deleteAll(ctx, func(ctx context.Context) (int64, error) {
		return s.store.DeleteResolvedAlertEventsBefore(ctx,
			now.AddDate(0, 0, -settings.AlertEventDays), model.ApplicationRetentionBatchSize)
	}); err != nil {
		failures = errors.Join(failures, fmt.Errorf("alert events: %w", err))
	}
	if counts.AuditEvents, err = s.deleteAll(ctx, func(ctx context.Context) (int64, error) {
		return s.store.DeleteAuditEventsBefore(ctx,
			now.AddDate(0, 0, -settings.AuditEventDays), model.ApplicationRetentionBatchSize)
	}); err != nil {
		failures = errors.Join(failures, fmt.Errorf("audit events: %w", err))
	}
	return counts, failures
}

// deleteAll 反复提交有界批次，直到没有更多可删行或上下文取消。
func (s *Service) deleteAll(
	ctx context.Context,
	batch func(context.Context) (int64, error),
) (int64, error) {
	total := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := batch(ctx)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(model.ApplicationRetentionBatchSize) {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(batchYield):
		}
	}
}

func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live
}

// automaticCleanupDue 判断当天计划时刻是否已经到达且尚未成功执行。
func automaticCleanupDue(overview model.ApplicationRetentionOverview, now time.Time) bool {
	if !overview.Settings.AutoCleanupEnabled || overview.Status.Running {
		return false
	}
	now = now.UTC()
	scheduled := time.Date(now.Year(), now.Month(), now.Day(),
		overview.Settings.CleanupHourUTC, 0, 0, 0, time.UTC)
	if now.Before(scheduled) {
		return false
	}
	return overview.Status.LastStartedAt == nil || overview.Status.LastStartedAt.Before(scheduled)
}

// errorCode 只暴露固定的失败类别，绝不记录底层 SQL 或路径。
func errorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "storage"
	}
}
