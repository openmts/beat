package model

import (
	"time"
)

// 诊断作业种类、协议、方向与受控地址族。
const (
	DiagnosticKindMTR    = "mtr"
	DiagnosticKindIperf3 = "iperf3"

	DiagnosticProtocolICMP = "icmp"
	DiagnosticProtocolTCP  = "tcp"
	DiagnosticProtocolUDP  = "udp"

	DiagnosticDirectionForward = "forward"
	DiagnosticDirectionReverse = "reverse"
)

// 诊断作业的持久状态机。
const (
	DiagnosticStateQueued      = "queued"
	DiagnosticStateLeased      = "leased"
	DiagnosticStateRunning     = "running"
	DiagnosticStateSucceeded   = "succeeded"
	DiagnosticStatePartial     = "partial"
	DiagnosticStateFailed      = "failed"
	DiagnosticStateTimedOut    = "timed_out"
	DiagnosticStateCancelled   = "cancelled"
	DiagnosticStateInterrupted = "interrupted"
	DiagnosticStateUnknown     = "unknown"
)

// 诊断的硬实现上限：被破坏的数据库值也不能超过这些边界。
const (
	DefaultDiagnosticProbes = 10
	MinimumDiagnosticProbes = 3
	MaximumDiagnosticProbes = 20

	DefaultDiagnosticHops = 32
	MaximumDiagnosticHops = 64

	DefaultIperfDurationSeconds          = 10
	MinimumDiagnosticDurationSeconds     = 3
	MaximumDiagnosticDurationSeconds     = 30
	HardMaximumDiagnosticDurationSeconds = 60

	MinimumDiagnosticStreams = 1
	MaximumDiagnosticStreams = 4

	DefaultDiagnosticBitrateMbps    = 100
	MaximumDiagnosticBitrateMbps    = 1000
	MaximumDiagnosticUDPBitrateMbps = 100

	DefaultMaximumActiveDiagnosticJobs = 2
	HardMaximumActiveDiagnosticJobs    = 4
	MaximumDiagnosticActivePerNode     = 1

	MaximumDiagnosticTargetAddresses = 16
	MaximumDiagnosticAddressBytes    = 255
	MaximumDiagnosticNameLength      = 100
	MaximumDiagnosticReferenceBytes  = 128
	MaximumDiagnosticPurposeLength   = 300
)

// DiagnosticPolicy 是服务端全局诊断策略，默认关闭全部诊断能力。
type DiagnosticPolicy struct {
	ID                       string    `json:"id"`
	Enabled                  bool      `json:"enabled"`
	AllowedKinds             []string  `json:"allowed_kinds"`
	AllowUDP                 bool      `json:"allow_udp"`
	AllowReverse             bool      `json:"allow_reverse"`
	AllowPrivateTargets      bool      `json:"allow_private_targets"`
	MaximumActiveJobs        int       `json:"maximum_active_jobs"`
	MaximumActiveJobsPerNode int       `json:"maximum_active_jobs_per_node"`
	DailyByteBudget          int64     `json:"daily_byte_budget"`
	MaximumIperfDuration     int       `json:"maximum_iperf_duration_seconds"`
	MaximumIperfBitrate      int       `json:"maximum_iperf_bitrate_mbps"`
	Revision                 int       `json:"revision"`
	UpdatedBy                string    `json:"updated_by"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// DefaultDiagnosticPolicy 返回关闭状态的服务端默认策略。
func DefaultDiagnosticPolicy() DiagnosticPolicy {
	return DiagnosticPolicy{ID: "default", AllowedKinds: []string{DiagnosticKindMTR},
		MaximumActiveJobs:        DefaultMaximumActiveDiagnosticJobs,
		MaximumActiveJobsPerNode: MaximumDiagnosticActivePerNode,
		MaximumIperfDuration:     MaximumDiagnosticDurationSeconds,
		MaximumIperfBitrate:      MaximumDiagnosticBitrateMbps,
		Revision:                 1}
}

// DiagnosticTarget 是 owner 审批过的诊断目标记录。
type DiagnosticTarget struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Purpose               string     `json:"purpose"`
	Kinds                 []string   `json:"kinds"`
	Address               string     `json:"address"`
	Port                  int        `json:"port"`
	IPFamily              string     `json:"ip_family"`
	Protocol              string     `json:"protocol"`
	Direction             string     `json:"direction"`
	PrivateAddress        bool       `json:"private_address"`
	AllowedAddresses      []string   `json:"allowed_addresses"`
	ResolvedAddresses     []string   `json:"resolved_addresses"`
	ResolvedAt            *time.Time `json:"resolved_at"`
	MaximumBitrateMbps    int        `json:"maximum_bitrate_mbps"`
	MaximumDuration       int        `json:"maximum_duration_seconds"`
	MaximumParallelStream int        `json:"maximum_parallel_streams"`
	DailyByteBudget       int64      `json:"daily_byte_budget"`
	Enabled               bool       `json:"enabled"`
	ExpiresAt             *time.Time `json:"expires_at"`
	Revision              int        `json:"revision"`
	CreatedBy             string     `json:"created_by"`
	UpdatedBy             string     `json:"updated_by"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// DiagnosticJob 是一次显式诊断作业，携带不可变目标/策略快照与分配。
type DiagnosticJob struct {
	ID               string     `json:"id"`
	SourceNodeID     string     `json:"source_node_id"`
	TargetID         string     `json:"target_id"`
	Kind             string     `json:"kind"`
	State            string     `json:"state"`
	ActorUserID      string     `json:"actor_user_id"`
	ActorUsername    string     `json:"actor_username"`
	Confirmation     string     `json:"confirmation"`
	SourceNodeName   string     `json:"source_node_name"`
	TargetName       string     `json:"target_name"`
	TargetRevision   int        `json:"target_revision"`
	PolicyRevision   int        `json:"policy_revision"`
	Protocol         string     `json:"protocol"`
	Direction        string     `json:"direction"`
	IPFamily         string     `json:"ip_family"`
	Probes           int        `json:"probes"`
	HopLimit         int        `json:"hop_limit"`
	DurationSeconds  int        `json:"duration_seconds"`
	ParallelStreams  int        `json:"parallel_streams"`
	BitrateMbps      int        `json:"bitrate_mbps"`
	PolicySnapshot   string     `json:"policy_snapshot"`
	TargetSnapshot   string     `json:"target_snapshot"`
	CapabilityDigest string     `json:"capability_digest"`
	ReservedBytes    int64      `json:"reserved_bytes"`
	ActualBytes      int64      `json:"actual_bytes"`
	Attempt          int        `json:"attempt"`
	CursorSequence   int64      `json:"cursor_sequence"`
	CancelRequested  bool       `json:"cancel_requested"`
	ErrorCode        string     `json:"error_code"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	LeasedAt         *time.Time `json:"leased_at"`
	StartedAt        *time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

// DiagnosticAttempt 记录一次租约尝试；到达 running 的尝试绝不自动重放。
type DiagnosticAttempt struct {
	JobID            string     `json:"job_id"`
	Attempt          int        `json:"attempt"`
	State            string     `json:"state"`
	LeaseNonce       string     `json:"lease_nonce"`
	CapabilityDigest string     `json:"capability_digest"`
	ErrorCode        string     `json:"error_code"`
	LeasedAt         time.Time  `json:"leased_at"`
	StartedAt        *time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

// DiagnosticTargetInput 是创建/更新目标的受控输入。
type DiagnosticTargetInput struct {
	Name                  string
	Purpose               string
	Kinds                 []string
	Address               string
	Port                  int
	IPFamily              string
	Protocol              string
	Direction             string
	PrivateAddress        bool
	AllowedAddresses      []string
	MaximumBitrateMbps    int
	MaximumDuration       int
	MaximumParallelStream int
	DailyByteBudget       int64
	Enabled               bool
	ExpiresAt             *time.Time
}
