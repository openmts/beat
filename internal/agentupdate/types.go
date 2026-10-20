package agentupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

var (
	ErrDisabled        = errors.New("agent update disabled")
	ErrManualOnly      = errors.New("agent update requires manual management")
	ErrExternalManager = errors.New("agent is externally managed")
	ErrConfiguration   = errors.New("invalid agent update configuration")
	ErrAssignment      = errors.New("invalid agent update assignment")
	// 分配给 Agent 的升级任务有三种彼此不同的拒绝原因。它们都包装 ErrAssignment，因此既有的
	// errors.Is(err, ErrAssignment) 判断与 HTTP 契约不变，只是日志与上报原因可以区分。
	ErrAssignmentEntrypoint = fmt.Errorf("%w: entrypoint does not match the managed executable", ErrAssignment)
	ErrAssignmentProtocol   = fmt.Errorf("%w: protocol range does not cover the agent protocol", ErrAssignment)
	ErrAssignmentArtifact   = fmt.Errorf("%w: manifest does not contain the requested artifact", ErrAssignment)
	ErrVersion              = errors.New("agent update version does not match assignment action")
	ErrBusy                 = errors.New("agent update already in progress")
	ErrStorage              = errors.New("agent update storage unavailable")
	ErrUnsafePath           = errors.New("unsafe agent update path")
	ErrCrossDevice          = errors.New("agent update requires one filesystem")
	ErrIntegrity            = errors.New("agent update image integrity failure")
	ErrFormat               = errors.New("agent update executable format mismatch")
	ErrDownload             = errors.New("agent update download failed")
	ErrJournal              = errors.New("agent update journal is invalid")
	ErrConflict             = errors.New("installed agent changed outside updater")
	ErrHealth               = errors.New("agent update health evidence rejected")
)

const ChunkBytes int64 = 4 << 20

const maximumJournalBytes = 2 << 20

const MaximumAssignmentLifetime = 24 * time.Hour

// Image 的version/size/digest必须来自带外安装的已验证元数据，不能现场给未知文件自背书。
type Image struct {
	Version string `json:"version"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// Manager 是固定服务管理适配器，不能接受Server提供的命令或服务名。
type Manager interface {
	Kind() string
	Restart(context.Context) error
}

type Config struct {
	Enabled               bool
	Container             bool
	Executable            string
	NodeID                string
	ServerURL             string
	Installed             Image
	Keys                  []protocol.SigningTrust
	Manager               Manager
	Fetcher               Fetcher
	OS, Arch              string
	AgentProtocol         int
	DownloadTimeout       time.Duration
	StartupTimeout        time.Duration
	ObservationWindow     time.Duration
	MaximumObservationGap time.Duration
	Now                   func() time.Time
}

type FetchRequest struct {
	Assignment     protocol.Assignment
	Offset, Length int64
}

type FetchResponse struct {
	Body                  io.ReadCloser
	Offset, Length, Total int64
}

type Fetcher interface {
	Fetch(context.Context, FetchRequest) (FetchResponse, error)
	TrustedServer() string
}

type Status struct {
	Phase            string     `json:"phase"`
	Reason           string     `json:"reason,omitempty"`
	AssignmentID     string     `json:"assignment_id,omitempty"`
	TargetVersion    string     `json:"target_version,omitempty"`
	PreviousVersion  string     `json:"previous_version,omitempty"`
	Downloaded       int64      `json:"downloaded"`
	WatchdogDeadline *time.Time `json:"watchdog_deadline,omitempty"`
}

type HealthEvidence struct {
	managerCounted                                                 bool
	ProcessID                                                      string
	Version                                                        string
	ConfigParsed, IdentityLoaded, ServerAuthenticated, ValidSample bool
	At                                                             time.Time
}

type Event struct {
	Phase, Reason string
	Bytes         int64
}

type Updater struct {
	config   Config
	mu       sync.Mutex
	observer func(Event)
	fault    func(string) error
}

func (u *Updater) SetObserver(observer func(Event)) { u.observer = observer }

func (u *Updater) observe(phase, reason string, bytes int64) {
	if u.observer != nil {
		u.observer(Event{Phase: phase, Reason: reason, Bytes: bytes})
	}
}

func (u *Updater) checkpoint(phase string) error {
	if u.fault != nil {
		return u.fault(phase)
	}
	return nil
}
