// Package metricsfanout 只生产共享公共快照并管理有界WebSocket连接。
package metricsfanout

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const (
	Cadence                   = 5 * time.Second
	StaleWindow               = 30 * time.Second
	BuildTimeout              = 30 * time.Second
	WriteTimeout              = 5 * time.Second
	ReadTimeout               = 75 * time.Second
	PingInterval              = 30 * time.Second
	MaximumSnapshotBytes      = 8 << 20
	MaximumInboundBytes       = 4 << 10
	MaximumHeaderBytes        = 16 << 10
	GlobalConnections         = 1024
	PerIPConnections          = 16
	MaximumTrackedIPs         = 4096
	GlobalHandshakeBurst      = 64
	GlobalHandshakesPerSecond = 32
	PerIPHandshakeBurst       = 8
	PerIPHandshakesPerSecond  = 1
	ipRetention               = 5 * time.Minute
	maximumControlBytes       = 125
	controlQueueCapacity      = 4
	frameBurst                = 32
	framesPerSecond           = 16
)

var ErrConfiguration = errors.New("invalid metrics fanout configuration")

var ErrClosed = errors.New("metrics fanout is closed")

var ErrRunning = errors.New("metrics fanout producer is already running")

var ErrBuilderPanic = errors.New("metrics fanout builder panicked")

var ErrObserverPanic = errors.New("metrics fanout observer panicked")

type Reason string

const (
	ReasonNone        Reason = ""
	ReasonBuilder     Reason = "builder"
	ReasonEncoding    Reason = "encoding"
	ReasonSnapshot    Reason = "invalid_snapshot"
	ReasonOversize    Reason = "oversize"
	ReasonPanic       Reason = "panic"
	ReasonClosed      Reason = "closed"
	ReasonOrigin      Reason = "origin"
	ReasonHandshake   Reason = "handshake"
	ReasonInvalidIP   Reason = "invalid_ip"
	ReasonGlobalLimit Reason = "global_limit"
	ReasonIPLimit     Reason = "ip_limit"
	ReasonRate        Reason = "handshake_rate"
	ReasonIPCapacity  Reason = "ip_capacity"
	ReasonCoalesced   Reason = "coalesced"
	ReasonStale       Reason = "stale"
	ReasonPeer        Reason = "peer"
	ReasonProtocol    Reason = "protocol"
	ReasonFrameRate   Reason = "frame_rate"
	ReasonTimeout     Reason = "timeout"
	ReasonWrite       Reason = "write"
)

type EventKind string

const (
	EventBuild      EventKind = "build"
	EventUpgrade    EventKind = "upgrade"
	EventConnection EventKind = "connection"
	EventDrop       EventKind = "drop"
	EventLag        EventKind = "lag"
)

// Event 仅含固定枚举、计数和时长，不能携带IP、节点、Origin或原始错误。
type Event struct {
	Kind     EventKind
	Reason   Reason
	Count    int64
	Duration time.Duration
}

type Snapshot struct {
	Value     any
	Timestamp time.Time
}

// 回调必须并发安全；Build必须遵守context取消，Observer必须快速返回。
type Config struct {
	Build       func(context.Context) (Snapshot, error)
	ClientIP    func(*http.Request) string
	AllowOrigin func(*http.Request) bool
	Observer    func(Event)
	Now         func() time.Time
}

type SnapshotInfo struct {
	Generation       uint64
	Timestamp        time.Time
	Age              time.Duration
	Stale, Available bool
}

type Stats struct {
	Running, Closed, ObserverFailed            bool
	Connections, Reserved, TrackedIPs          int
	Generation                                 SnapshotInfo
	LastAttempt, LastSuccess, LastFailure      time.Time
	Failure                                    Reason
	ConsecutiveFailures                        uint64
	Builds, Failures, SkippedCadences, Dropped uint64
	BuildDuration, SchedulerLag                time.Duration
}
