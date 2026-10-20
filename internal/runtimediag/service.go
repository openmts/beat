package runtimediag

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// CaptureFunc 把指定类型的 profile 写入 writer；实现必须是可取消且不改变全局采样率。
type CaptureFunc func(ctx context.Context, kind string, duration time.Duration, writer io.Writer) error

// Config 固定服务行为；所有可选依赖缺失时使用真实运行时实现。
type Config struct {
	Directory string
	Version   string
	Commit    string
	StartedAt time.Time
	Limits    Limits
	Now       func() time.Time
	Capture   CaptureFunc
	Readiness func() error
	Observer  Observer
}

type entry struct {
	Capture
	path      string
	cancel    context.CancelFunc
	done      chan struct{}
	file      *os.File
	consuming bool
}

// Service 持有固定上限、内存注册表与私有目录；重启即丢弃并在启动时清理孤儿文件。
type Service struct {
	config Config
	limits Limits
	now    func() time.Time
	mu     sync.Mutex
	items  map[string]*entry
	rates  map[string][]time.Time
	active int
	closed bool
	wait   sync.WaitGroup

	observer        atomic.Pointer[Observer]
	cleanupFailures uint64
}

// New 校验配置、创建 0700 目录并清理上次进程留下的全部诊断文件。
func New(config Config) (*Service, error) {
	if config.Directory == "" || config.Capture == nil {
		return nil, errors.New("runtime diagnostics directory and capture function are required")
	}
	if config.Limits == (Limits{}) {
		config.Limits = DefaultLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return nil, err
	}
	directory, err := filepath.Abs(config.Directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, err
	}
	config.Directory = directory
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if config.StartedAt.IsZero() {
		config.StartedAt = now()
	}
	service := &Service{config: config, limits: config.Limits, now: now, items: map[string]*entry{},
		rates: map[string][]time.Time{}}
	if config.Observer != nil {
		service.SetObserver(config.Observer)
	}
	if err := service.removeOrphans(); err != nil {
		return nil, err
	}
	return service, nil
}

// removeOrphans 只删除私有目录内的常规文件；符号链接与其他类型保持可见并阻断新捕获。
func (service *Service) removeOrphans() error {
	entries, err := os.ReadDir(service.config.Directory)
	if err != nil {
		return err
	}
	for _, item := range entries {
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("runtime diagnostics directory holds an unexpected entry")
		}
		if err := os.Remove(filepath.Join(service.config.Directory, item.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Start 校验类型与时长、执行并发和速率限制，然后异步写入私有文件。
func (service *Service) Start(ctx context.Context, ownerID, sessionID, kind string, duration time.Duration) (Capture, error) {
	if !KindIsSupported(kind) {
		service.observe(Event{Kind: kind, Result: EventRejected, Code: CodeInvalidKind})
		return Capture{}, Error{Code: CodeInvalidKind}
	}
	if !service.limits.durationIsValid(kind, duration) {
		service.observe(Event{Kind: kind, Result: EventRejected, Code: CodeInvalidDuration})
		return Capture{}, Error{Code: CodeInvalidDuration}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.now()
	service.sweepLocked(now)
	if service.closed {
		return Capture{}, Error{Code: CodeCapacity}
	}
	if err := service.admitLocked(ownerID, kind, now); err != nil {
		service.observe(Event{Kind: kind, Result: EventRejected, Code: CodeOf(err)})
		return Capture{}, err
	}
	file, err := os.CreateTemp(service.config.Directory, "capture-*.prof")
	if err != nil {
		service.observe(Event{Kind: kind, Result: EventRejected, Code: CodeCapacity})
		return Capture{}, Error{Code: CodeCapacity}
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		service.observe(Event{Kind: kind, Result: EventRejected, Code: CodeCapacity})
		return Capture{}, Error{Code: CodeCapacity}
	}
	captureContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	item := &entry{Capture: Capture{ID: identifier(), Kind: kind, Status: StatusRunning, StartedAt: now,
		Deadline: now.Add(duration), ExpiresAt: now.Add(service.limits.Expiry), OwnerID: ownerID, SessionID: sessionID},
		path: file.Name(), file: file, cancel: cancel, done: make(chan struct{})}
	service.items[item.ID] = item
	service.active++
	service.wait.Add(1)
	go service.run(captureContext, item, duration)
	service.observe(Event{Kind: kind, Result: EventStarted})
	return item.Capture, nil
}

func (service *Service) run(ctx context.Context, item *entry, duration time.Duration) {
	defer service.wait.Done()
	defer close(item.done)
	writer := &boundedWriter{target: item.file, remaining: service.limits.MaximumArtifactBytes}
	err := service.config.Capture(ctx, item.Kind, duration, writer)
	closeErr := item.file.Close()
	item.file = nil
	service.finish(item, err, closeErr, writer)
}

func (service *Service) finish(item *entry, captureErr, closeErr error, writer *boundedWriter) {
	service.mu.Lock()
	defer service.mu.Unlock()
	completed := service.now()
	switch {
	case captureErr == nil && closeErr == nil:
		item.Status, item.Bytes = StatusComplete, writer.written
		item.ExpiresAt = completed.Add(service.limits.Expiry)
	case writer.exceeded:
		service.failLocked(item, CodeSizeExceeded)
	case errors.Is(captureErr, context.Canceled):
		service.failLocked(item, "")
		item.Status = StatusCancelled
	default:
		service.failLocked(item, CodeWriterFailed)
	}
	if item.Status != StatusComplete {
		service.removeFile(item)
	}
	service.active--
	service.observe(finishEvent(item))
}

// finishEvent 把终态映射为固定事件，失败代码保持枚举值。
func finishEvent(item *entry) Event {
	event := Event{Kind: item.Kind, Result: EventFailed, Code: item.FailureCode, Bytes: item.Bytes}
	switch item.Status {
	case StatusComplete:
		event.Result = EventCompleted
	case StatusCancelled:
		event.Result = EventCancelled
	default:
		if event.Code == "" {
			event.Code = CodeWriterFailed
		}
	}
	return event
}

func (service *Service) failLocked(item *entry, code string) {
	item.Status, item.FailureCode, item.Bytes = StatusFailed, code, 0
	if code == "" {
		item.FailureCode = ""
	}
}

// removeFile 删除产物；调用方必须持有锁，这样清理失败计数与目录状态保持一致。
func (service *Service) removeFile(item *entry) {
	if item.path == "" {
		return
	}
	if err := os.Remove(item.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		service.cleanupFailures++
		slog.Error("runtime diagnostics artifact cleanup failed", "reason", "cleanup_failed")
	}
}

// List 返回该所有者可见的捕获元数据，并在返回前清理过期条目。
func (service *Service) List(ownerID string) []Capture {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.sweepLocked(service.now())
	items := make([]Capture, 0, len(service.items))
	for _, item := range service.items {
		if item.OwnerID == ownerID {
			items = append(items, item.Capture)
		}
	}
	sort.Slice(items, func(left, right int) bool { return items[left].StartedAt.After(items[right].StartedAt) })
	return items
}

// Busy 报告全局是否仍有运行的捕获。
func (service *Service) Busy() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.active > 0
}

func identifier() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return hex.EncodeToString(buffer)
}
