package model

import (
	"errors"
	"time"
)

// MaximumReleaseRetentionDays 上限十年，拦截把秒数或毫秒数写成天数的误配。
const MaximumReleaseRetentionDays = 3650

// 保留期自动清理最近一轮的状态取值。
const (
	AgentRetentionStatusIdle    = "idle"
	AgentRetentionStatusRunning = "running"
	AgentRetentionStatusOK      = "ok"
	AgentRetentionStatusFailed  = "failed"
)

// 触发来源：调度器周期任务与管理员手动触发。
const (
	AgentRetentionTriggerSchedule = "schedule"
	AgentRetentionTriggerManual   = "manual"
)

var ErrAgentRetentionInvalid = errors.New("invalid agent release retention settings")

// AgentRetentionDefaults 是启动环境变量给出的缺省保留期；后台没有显式配置时生效。
type AgentRetentionDefaults struct {
	RolloutDays int  `json:"rollout_days"`
	ReleaseDays int  `json:"release_days"`
	Enabled     bool `json:"enabled"`
}

// AgentRetentionRunStatus 汇总最近一轮自动清理的结果，供后台展示。
type AgentRetentionRunStatus struct {
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastStatus    string     `json:"last_status"`
	LastError     string     `json:"last_error"`
	LastRollouts  int        `json:"last_rollouts"`
	LastReleases  int        `json:"last_releases"`
	LastArtifacts int        `json:"last_artifacts"`
	LastTrigger   string     `json:"last_trigger"`
}

// AgentRetentionSettings 是后台可配置的发布对象保留期；Configured=false 表示仍在用环境变量缺省。
type AgentRetentionSettings struct {
	Configured  bool                    `json:"configured"`
	RolloutDays int                     `json:"rollout_days"`
	ReleaseDays int                     `json:"release_days"`
	UpdatedAt   time.Time               `json:"updated_at"`
	Environment AgentRetentionDefaults  `json:"environment"`
	Status      AgentRetentionRunStatus `json:"status"`
}

// Validate 检查保留期是否在允许区间内；0 表示保留全部历史。
func (settings AgentRetentionSettings) Validate() error {
	if !ValidRetentionDays(settings.RolloutDays) || !ValidRetentionDays(settings.ReleaseDays) {
		return ErrAgentRetentionInvalid
	}
	return nil
}

// ValidRetentionDays 报告单个保留期天数是否合法。
func ValidRetentionDays(days int) bool {
	return days >= 0 && days <= MaximumReleaseRetentionDays
}

// Enabled 报告这份设置是否会真正清理对象。
func (settings AgentRetentionSettings) Enabled() bool {
	return settings.RolloutDays > 0 || settings.ReleaseDays > 0
}
