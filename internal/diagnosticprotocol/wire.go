package diagnosticprotocol

// Lease 是服务端派发的固定参数集合；没有自由命令字段。
type Lease struct {
	ExpiresAt int64      `json:"expires_at"`
	JobID     string     `json:"job_id"`
	Nonce     string     `json:"nonce"`
	Attempt   int        `json:"attempt"`
	Payload   JobPayload `json:"payload"`
}

type JobPayload struct {
	Kind             string `json:"kind"`
	Destination      string `json:"destination"`
	Protocol         string `json:"protocol"`
	Direction        string `json:"direction"`
	IPFamily         string `json:"ip_family"`
	TargetName       string `json:"target_name"`
	CapabilityDigest string `json:"capability_digest"`
	Port             int    `json:"port"`
	Probes           int    `json:"probes"`
	HopLimit         int    `json:"hop_limit"`
	DurationSeconds  int    `json:"duration_seconds"`
	ParallelStreams  int    `json:"parallel_streams"`
	BitrateMbps      int    `json:"bitrate_mbps"`
	TargetRevision   int    `json:"target_revision"`
	PolicyRevision   int    `json:"policy_revision"`
}

// Sample 是一条数值样本；作业与节点身份由服务端填充，Agent 不得声明。
type Sample struct {
	Metric    string  `json:"metric"`
	Stat      string  `json:"stat,omitempty"`
	Role      string  `json:"role,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Protocol  string  `json:"protocol,omitempty"`
	Hop       int     `json:"hop,omitempty"`
	Value     float64 `json:"value"`
}

// HopIdentity 是单个跳的身份；只在管理员详情中解密展示。
type HopIdentity struct {
	Hostname string `json:"hostname,omitempty"`
	Address  string `json:"address,omitempty"`
	Index    int    `json:"index"`
	Reached  bool   `json:"reached"`
}

// ProgressRequest 是进度与结果批次；序号必须单调递增。
type ProgressRequest struct {
	JobID    string        `json:"job_id"`
	Nonce    string        `json:"nonce"`
	Samples  []Sample      `json:"samples"`
	Hops     []HopIdentity `json:"hops"`
	Attempt  int           `json:"attempt"`
	Sequence int64         `json:"sequence"`
}

// CompletionRequest 是终态报告。
type CompletionRequest struct {
	JobID       string `json:"job_id"`
	Nonce       string `json:"nonce"`
	State       string `json:"state"`
	FailureCode string `json:"failure_code"`
	Attempt     int    `json:"attempt"`
	ActualBytes int64  `json:"actual_bytes"`
}

// AttemptRequest 认证一次尝试；start 与心跳复用同一形状。
type AttemptRequest struct {
	JobID   string `json:"job_id"`
	Nonce   string `json:"nonce"`
	Attempt int    `json:"attempt"`
}

// LeaseRequest 与 AdvertiseRequest 是 Agent 的发起点。
type LeaseRequest struct {
	CapabilityDigest string `json:"capability_digest"`
}

type AdvertiseRequest struct {
	CapabilityDocument
}

type AdvertiseResponse struct {
	Digest string `json:"digest"`
}

type ProgressResponse struct {
	Sequence int64 `json:"sequence"`
}

type HeartbeatResponse struct {
	CancelRequested bool `json:"cancel_requested"`
}

type Acknowledgement struct {
	Acknowledged bool `json:"acknowledged"`
}
