package handler

import (
	"context"
	"net/http"
	"time"
)

// 只读迁移视图的固定状态与代码：浏览器永远看不到 SQL、路径或原始错误文本。
const (
	MigrationStateCurrent        = "current"
	MigrationStateUpgradePending = "upgrade_pending"
	MigrationStatePendingRestore = "pending_restore"
	MigrationStateInconsistent   = "inconsistent"
	MigrationCodeOK              = "ok"
	MigrationCodePendingRestore  = "pending_restore"
	MigrationCodeSchemaMismatch  = "schema_mismatch"
	MigrationCodeUnreadable      = "database_unreadable"
	MigrationCodeRestoreUnknown  = "restore_state_unknown"
	migrationPendingLimit        = 64
)

// MigrationStepView 是只读清单里的一条迁移。
type MigrationStepView struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

// MigrationFailureView 是脱敏后的最近一次失败标记。
type MigrationFailureView struct {
	UpdatedAt        time.Time `json:"updated_at"`
	State            string    `json:"state"`
	Phase            string    `json:"phase"`
	FailureCode      string    `json:"failure_code"`
	MigrationName    string    `json:"migration_name"`
	SourceSchema     int       `json:"source_schema"`
	TargetSchema     int       `json:"target_schema"`
	RollbackRequired bool      `json:"rollback_required"`
}

// MigrationOverviewView 是所有者可见的只读迁移与恢复视图。
type MigrationOverviewView struct {
	LastFailure            *MigrationFailureView `json:"last_failure,omitempty"`
	Version                string                `json:"version"`
	State                  string                `json:"state"`
	Code                   string                `json:"code"`
	RunbookHash            string                `json:"runbook_hash"`
	ApplicationHealth      string                `json:"application_health"`
	MetricsHealth          string                `json:"metrics_health"`
	CurrentSchema          int                   `json:"current_schema"`
	MinimumSupportedSchema int                   `json:"minimum_supported_schema"`
	MaximumSupportedSchema int                   `json:"maximum_supported_schema"`
	LatestCompleted        *MigrationStepView    `json:"latest_completed,omitempty"`
	PendingMigrations      []MigrationStepView   `json:"pending_migrations"`
	RestorePending         bool                  `json:"restore_pending"`
	RestoreStaged          bool                  `json:"restore_staged"`
}

// MigrationOverviewSource 提供只读迁移视图；实现由服务端装配。
type MigrationOverviewSource interface {
	MigrationOverview(ctx context.Context) (MigrationOverviewView, error)
}

// MigrationOverviewHandler 只读输出迁移与恢复状态；路由层已强制 owner 会话。
type MigrationOverviewHandler struct{ source MigrationOverviewSource }

func NewMigrationOverviewHandler(source MigrationOverviewSource) *MigrationOverviewHandler {
	return &MigrationOverviewHandler{source: source}
}

func (h *MigrationOverviewHandler) HandleOverview(w http.ResponseWriter, request *http.Request) {
	if h.source == nil {
		JSONError(w, http.StatusServiceUnavailable, "migration state is unavailable")
		return
	}
	view, err := h.source.MigrationOverview(request.Context())
	if err != nil {
		JSONError(w, http.StatusServiceUnavailable, "migration state is unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, view)
}

// MigrationSteps 把迁移清单限制在固定条数内，避免响应被无界列表撑大。
func MigrationSteps(pending []MigrationStepView) []MigrationStepView {
	if pending == nil {
		return []MigrationStepView{}
	}
	if len(pending) > migrationPendingLimit {
		return pending[:migrationPendingLimit]
	}
	return pending
}
