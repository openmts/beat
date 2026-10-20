package model

import (
	"fmt"
	"time"
)

const (
	ApplicationRetentionStatusNever   = "never"
	ApplicationRetentionStatusRunning = "running"
	ApplicationRetentionStatusSuccess = "success"
	ApplicationRetentionStatusFailed  = "failed"

	ApplicationRetentionTriggerManual    = "manual"
	ApplicationRetentionTriggerAutomatic = "automatic"

	// ApplicationRetentionBatchSize 限定每个短事务删除的行数。
	ApplicationRetentionBatchSize = 500
	// ApplicationSessionRetentionDays 是吊销会话的固定清理窗口（安全策略，不对外开放）。
	ApplicationSessionRetentionDays = 30
	// ApplicationRetentionMinimumDays 与 ApplicationRetentionMaximumDays 约束可配置保留天数。
	ApplicationRetentionMinimumDays = 1
	ApplicationRetentionMaximumDays = 3650
)

// ApplicationRetentionSettings 是应用历史（告警事件与管理员审计）的保留策略。
type ApplicationRetentionSettings struct {
	AlertEventDays     int       `json:"alert_event_days"`
	AuditEventDays     int       `json:"audit_event_days"`
	CleanupHourUTC     int       `json:"cleanup_hour_utc"`
	AutoCleanupEnabled bool      `json:"auto_cleanup_enabled"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ApplicationRetentionStatus struct {
	Running         bool       `json:"running"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastCompletedAt *time.Time `json:"last_completed_at"`
	LastStatus      string     `json:"last_status"`
	LastErrorCode   string     `json:"last_error_code"`
	LastTrigger     string     `json:"last_trigger"`
	LastDurationMS  int64      `json:"last_duration_ms"`
	DeletedAlerts   int64      `json:"deleted_alert_events"`
	DeletedAudits   int64      `json:"deleted_audit_events"`
	DeletedSessions int64      `json:"deleted_sessions"`
}

// ApplicationRetentionCounts 汇总一次清理实际删除的行数。
type ApplicationRetentionCounts struct {
	AlertEvents int64
	AuditEvents int64
	Sessions    int64
}

type ApplicationRetentionOverview struct {
	Settings ApplicationRetentionSettings `json:"settings"`
	Status   ApplicationRetentionStatus   `json:"status"`
	// SessionRetentionDays 是固定安全策略的只读回显；备份终态记录的固定窗口随备份生命周期批次一起落地。
	SessionRetentionDays int `json:"session_retention_days"`
}

func DefaultApplicationRetentionSettings() ApplicationRetentionSettings {
	return ApplicationRetentionSettings{AlertEventDays: 180, AuditEventDays: 180, CleanupHourUTC: 3, AutoCleanupEnabled: true}
}

// Validate 校验可配置范围；会话与备份的固定窗口不在此处暴露。
func (settings ApplicationRetentionSettings) Validate() error {
	if settings.AlertEventDays < ApplicationRetentionMinimumDays ||
		settings.AlertEventDays > ApplicationRetentionMaximumDays {
		return fmt.Errorf("alert event days must be between %d and %d",
			ApplicationRetentionMinimumDays, ApplicationRetentionMaximumDays)
	}
	if settings.AuditEventDays < ApplicationRetentionMinimumDays ||
		settings.AuditEventDays > ApplicationRetentionMaximumDays {
		return fmt.Errorf("audit event days must be between %d and %d",
			ApplicationRetentionMinimumDays, ApplicationRetentionMaximumDays)
	}
	if settings.CleanupHourUTC < 0 || settings.CleanupHourUTC > 23 {
		return fmt.Errorf("cleanup hour must be between 0 and 23")
	}
	return nil
}
