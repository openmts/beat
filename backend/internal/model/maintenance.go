package model

import (
	"errors"
	"time"
)

const (
	MaintenanceStatusNever   = "never"
	MaintenanceStatusRunning = "running"
	MaintenanceStatusSuccess = "success"
	MaintenanceStatusFailed  = "failed"

	MaintenanceTriggerManual    = "manual"
	MaintenanceTriggerAutomatic = "automatic"
)

type MaintenanceSettings struct {
	RetentionDays      int       `json:"retention_days"`
	AutoCleanupEnabled bool      `json:"auto_cleanup_enabled"`
	CleanupHourUTC     int       `json:"cleanup_hour_utc"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type MaintenanceRunStatus struct {
	Running         bool       `json:"running"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastCompletedAt *time.Time `json:"last_completed_at"`
	LastStatus      string     `json:"last_status"`
	LastError       string     `json:"last_error"`
	LastCutoffAt    *time.Time `json:"last_cutoff_at"`
	LastDurationMS  int64      `json:"last_duration_ms"`
	LastTrigger     string     `json:"last_trigger"`
	SQLiteIntegrity string     `json:"sqlite_integrity"`
}

type StorageUsage struct {
	MTSBytes         int64    `json:"mts_bytes"`
	SQLiteBytes      int64    `json:"sqlite_bytes"`
	TotalBytes       int64    `json:"total_bytes"`
	MTSHealthy       bool     `json:"mts_healthy"`
	MTSHealthReasons []string `json:"mts_health_reasons"`
}

type MaintenanceOverview struct {
	Settings MaintenanceSettings  `json:"settings"`
	Status   MaintenanceRunStatus `json:"status"`
	Storage  StorageUsage         `json:"storage"`
}

func DefaultMaintenanceSettings() MaintenanceSettings {
	return MaintenanceSettings{
		RetentionDays:      30,
		AutoCleanupEnabled: true,
		CleanupHourUTC:     3,
	}
}

func (settings MaintenanceSettings) Validate() error {
	if settings.RetentionDays < 1 || settings.RetentionDays > 3650 {
		return errors.New("retention days must be between 1 and 3650")
	}
	if settings.CleanupHourUTC < 0 || settings.CleanupHourUTC > 23 {
		return errors.New("cleanup hour must be between 0 and 23")
	}
	return nil
}
