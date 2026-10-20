package model

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// 擦除范围是封闭枚举：API 永远不接受 measurement、tag、时间范围或谓词。
const (
	MetricErasureNodeHistory        = "node_history"
	MetricErasureNetworkTaskHistory = "network_task_history"
	MetricErasureFleetTelemetry     = "fleet_telemetry"
	MetricErasureAllMetricHistory   = "all_metric_history"
	MetricErasureDeleteNode         = "delete_node"
	MetricErasureDeleteNetworkTask  = "delete_network_task"
)

// 任务状态机。只有 queued 可以取消；一旦产生 tombstone 就必须跑到终态或重试。
const (
	MetricErasureQueued     = "queued"
	MetricErasureClaimed    = "claimed"
	MetricErasureDeleting   = "deleting"
	MetricErasureCompacting = "compacting"
	MetricErasureFinalizing = "finalizing"
	MetricErasureSucceeded  = "succeeded"
	MetricErasureFailed     = "failed"
	MetricErasureCanceled   = "canceled"
)

// 舰队级擦除要求固定短语确认；实体级擦除要求精确显示名确认。
const (
	MetricErasureFleetConfirmation = "ERASE FLEET TELEMETRY"
	MetricErasureAllConfirmation   = "ERASE ALL METRIC HISTORY"
)

// 目标类型与有界文本上限；审计快照只保存类型、ID 与显示名。
const (
	MetricErasureTargetNone        = ""
	MetricErasureTargetNode        = "node"
	MetricErasureTargetNetworkTask = "network_task"

	MaximumMetricErasureNameBytes   = 128
	MaximumMetricErasureIDBytes     = 64
	MaximumMetricErasureErrorBytes  = 64
	MaximumMetricErasureDigestBytes = 64
)

// 对外只暴露脱敏错误码；原始错误文本永不进入 API、日志字段或审计详情。
const (
	MetricErasureErrorMTS        = "mts_delete_failed"
	MetricErasureErrorCompaction = "mts_compaction_failed"
	MetricErasureErrorFinalize   = "finalize_failed"
	MetricErasureErrorInventory  = "inventory_mismatch"
	MetricErasureErrorLock       = "mts_lock_unavailable"
	MetricErasureErrorLease      = "lease_expired"
	MetricErasureErrorInternal   = "internal_error"
)

var (
	ErrMetricErasureInvalid  = errors.New("invalid metric erasure request")
	ErrMetricErasureConflict = errors.New("metric erasure conflict")
	ErrMetricErasureMissing  = errors.New("metric erasure job not found")
)

// MetricErasureJob 是一行耐久擦除任务；它只保存元数据，绝不保存指标样本。
type MetricErasureJob struct {
	ID                string
	Scope             string
	TargetType        string
	TargetID          string
	TargetName        string
	State             string
	CutoffUnixNano    *int64
	CatalogDigest     string
	MeasurementCursor string
	AttemptCount      int
	LeaseOwner        string
	LeaseExpiresAt    *time.Time
	NextAttemptAt     *time.Time
	RequestID         string
	ActorID           string
	ActorName         string
	SessionPrefix     string
	LastErrorCode     string
	RequestedAt       time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
}

// Active 报告任务是否仍占用耐久资源：活动任务不能被第二个同类任务覆盖。
func (job MetricErasureJob) Active() bool {
	switch job.State {
	case MetricErasureQueued, MetricErasureClaimed, MetricErasureDeleting,
		MetricErasureCompacting, MetricErasureFinalizing:
		return true
	default:
		return false
	}
}

// Succeeded 报告任务是否已经产生不可逆的 MTS tombstone。
func (job MetricErasureJob) Succeeded() bool { return job.State == MetricErasureSucceeded }

// ErasesEntity 报告成功擦除后是否还要删除实体应用行。
func (job MetricErasureJob) ErasesEntity() bool {
	return job.Scope == MetricErasureDeleteNode || job.Scope == MetricErasureDeleteNetworkTask
}

// MetricErasureScopes 返回全部合法范围，顺序固定便于渲染与测试。
func MetricErasureScopes() []string {
	scopes := []string{MetricErasureNodeHistory, MetricErasureNetworkTaskHistory,
		MetricErasureFleetTelemetry, MetricErasureAllMetricHistory,
		MetricErasureDeleteNode, MetricErasureDeleteNetworkTask}
	sort.Strings(scopes)
	return scopes
}

// MetricErasureTargetTypeFor 返回范围要求的目标类型；空串表示舰队级范围。
func MetricErasureTargetTypeFor(scope string) string {
	switch scope {
	case MetricErasureNodeHistory, MetricErasureDeleteNode:
		return MetricErasureTargetNode
	case MetricErasureNetworkTaskHistory, MetricErasureDeleteNetworkTask:
		return MetricErasureTargetNetworkTask
	default:
		return MetricErasureTargetNone
	}
}

// MetricErasureConfirmationFor 返回舰队级范围的固定确认短语；实体级返回空串，
// 由调用方改用精确显示名。
func MetricErasureConfirmationFor(scope string) string {
	switch scope {
	case MetricErasureFleetTelemetry:
		return MetricErasureFleetConfirmation
	case MetricErasureAllMetricHistory:
		return MetricErasureAllConfirmation
	default:
		return ""
	}
}

// ValidMetricErasureScope 判断范围是否在封闭枚举内。
func ValidMetricErasureScope(scope string) bool {
	return MetricErasureTargetTypeFor(scope) != MetricErasureTargetNone ||
		scope == MetricErasureFleetTelemetry || scope == MetricErasureAllMetricHistory
}

// MetricErasureCreateRequest 是一次显式擦除请求；确认值按范围逐字比对。
type MetricErasureCreateRequest struct {
	Scope        string
	TargetID     string
	RequestID    string
	Confirmation string
	TargetName   string
}

// Validate 校验范围、目标形状与确认短语；TargetName 由服务端解析后回填。
func (request MetricErasureCreateRequest) Validate() error {
	if !ValidMetricErasureScope(request.Scope) || !ValidUUIDShape(request.RequestID) {
		return ErrMetricErasureInvalid
	}
	targetType := MetricErasureTargetTypeFor(request.Scope)
	if targetType == MetricErasureTargetNone {
		if request.TargetID != "" {
			return ErrMetricErasureInvalid
		}
		if request.Confirmation != MetricErasureConfirmationFor(request.Scope) {
			return ErrMetricErasureInvalid
		}
		return nil
	}
	if request.TargetID == "" || len(request.TargetID) > MaximumMetricErasureIDBytes {
		return ErrMetricErasureInvalid
	}
	if request.TargetName == "" || len(request.TargetName) > MaximumMetricErasureNameBytes ||
		strings.ContainsRune(request.TargetName, 0) {
		return ErrMetricErasureInvalid
	}
	if request.Confirmation != request.TargetName {
		return ErrMetricErasureInvalid
	}
	return nil
}

// ValidMetricErasureDigest 接受十六进制的测量清单摘要。
func ValidMetricErasureDigest(value string) bool {
	if len(value) != MaximumMetricErasureDigestBytes {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
