// Package runtimediag 提供仅所有者可用的有界运行时诊断：摘要与临时 profile 捕获。
// profile 字节只落在私有目录文件里，绝不进入 SQLite、MTS、备份或日志。
package runtimediag

import (
	"errors"
	"fmt"
	"time"
)

// 支持的捕获类型是固定枚举；请求只能选择其中之一，不能传任意 profile 名。
const (
	KindCPU          = "cpu"
	KindTrace        = "trace"
	KindSummary      = "summary"
	KindHeap         = "heap"
	KindAllocs       = "allocs"
	KindGoroutine    = "goroutine"
	KindMutex        = "mutex"
	KindBlock        = "block"
	KindThreadCreate = "threadcreate"
)

// 捕获状态与固定失败代码。
const (
	StatusRunning   = "running"
	StatusComplete  = "complete"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"

	CodeConflict        = "conflict"
	CodeRateLimited     = "rate_limited"
	CodeInvalidKind     = "invalid_kind"
	CodeInvalidDuration = "invalid_duration"
	CodeCapacity        = "capacity"
	CodeSizeExceeded    = "size_limit"
	CodeWriterFailed    = "writer_failed"
	CodeNotFound        = "not_found"
)

// 默认上限；部署可以下调，不能在没有评审的情况下上调。
const (
	DefaultMinimumDuration       = 1 * time.Second
	DefaultMaximumDuration       = 30 * time.Second
	DefaultMinimumCPUDuration    = 5 * time.Second
	DefaultMaximumTraceDuration  = 5 * time.Second
	DefaultMaximumArtifactBytes  = 64 << 20
	DefaultMaximumDirectoryBytes = 128 << 20
	DefaultExpiry                = 15 * time.Minute
	instantaneousPerMinute       = 2
)

// Error 是带固定代码的捕获错误；HTTP 层只依据代码映射状态，不泄露底层文本。
type Error struct {
	Code string
}

func (failure Error) Error() string { return "runtime diagnostics: " + failure.Code }

// CodeOf 返回固定错误代码；未知错误按内部失败处理。
func CodeOf(err error) string {
	var failure Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	if err == nil {
		return ""
	}
	return CodeWriterFailed
}

// KindIsTimed 区分需要持续采样的类型与瞬时类型。
func KindIsTimed(kind string) bool { return kind == KindCPU || kind == KindTrace }

// KindIsSupported 校验类型是否在固定枚举内。
func KindIsSupported(kind string) bool {
	switch kind {
	case KindCPU, KindTrace, KindHeap, KindAllocs, KindGoroutine, KindMutex, KindBlock, KindThreadCreate:
		return true
	default:
		return false
	}
}

// Capture 是对外暴露的元数据；它不含文件路径与 profile 内容。
type Capture struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	FailureCode string    `json:"failure_code,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	Deadline    time.Time `json:"deadline"`
	ExpiresAt   time.Time `json:"expires_at"`
	Bytes       int64     `json:"bytes"`
	OwnerID     string    `json:"owner_id"`
	SessionID   string    `json:"-"`
}

// Limits 是构造时固定下来的有界参数。
type Limits struct {
	MinimumDuration       time.Duration
	MaximumDuration       time.Duration
	MinimumCPUDuration    time.Duration
	MaximumTraceDuration  time.Duration
	MaximumArtifactBytes  int64
	MaximumDirectoryBytes int64
	Expiry                time.Duration
}

// DefaultLimits 返回部署默认上限。
func DefaultLimits() Limits {
	return Limits{MinimumDuration: DefaultMinimumDuration, MaximumDuration: DefaultMaximumDuration,
		MinimumCPUDuration: DefaultMinimumCPUDuration, MaximumTraceDuration: DefaultMaximumTraceDuration,
		MaximumArtifactBytes:  DefaultMaximumArtifactBytes,
		MaximumDirectoryBytes: DefaultMaximumDirectoryBytes, Expiry: DefaultExpiry}
}

// Validate 拒绝零值、负值与内部不一致的上限，避免静默关闭保护。
func (limits Limits) Validate() error {
	switch {
	case limits.MinimumDuration <= 0 || limits.MaximumDuration < limits.MinimumDuration:
		return fmt.Errorf("runtime diagnostics duration limits are invalid")
	case limits.MinimumCPUDuration < limits.MinimumDuration || limits.MinimumCPUDuration > limits.MaximumDuration:
		return fmt.Errorf("runtime diagnostics CPU duration limit is invalid")
	case limits.MaximumTraceDuration < limits.MinimumDuration || limits.MaximumTraceDuration > limits.MaximumDuration:
		return fmt.Errorf("runtime diagnostics trace duration limit is invalid")
	case limits.MaximumArtifactBytes <= 0 || limits.MaximumDirectoryBytes < limits.MaximumArtifactBytes:
		return fmt.Errorf("runtime diagnostics byte limits are invalid")
	case limits.Expiry <= 0:
		return fmt.Errorf("runtime diagnostics expiry is invalid")
	default:
		return nil
	}
}

// durationBounds 返回某一类型的固定时长范围；瞬时类型必须传 0。
func (limits Limits) durationBounds(kind string) (time.Duration, time.Duration, bool) {
	switch kind {
	case KindCPU:
		return limits.MinimumCPUDuration, limits.MaximumDuration, true
	case KindTrace:
		return limits.MinimumDuration, limits.MaximumTraceDuration, true
	default:
		return 0, 0, false
	}
}

func (limits Limits) durationIsValid(kind string, duration time.Duration) bool {
	minimum, maximum, timed := limits.durationBounds(kind)
	if !timed {
		return duration == 0
	}
	return duration >= minimum && duration <= maximum
}
