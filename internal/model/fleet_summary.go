package model

import "time"

const (
	FleetSummaryDaily  = "daily"
	FleetSummaryWeekly = "weekly"

	FleetSummaryResultQueued  = "queued"
	FleetSummaryResultSuccess = "success"
	FleetSummaryResultPartial = "partial"
	FleetSummaryResultFailed  = "failed"
	FleetSummaryResultUnknown = "unknown"

	FleetSummaryRunScheduled = "scheduled"
	FleetSummaryRunTest      = "test"

	FleetSummaryDefaultNameThreshold = 10
	FleetSummaryMaximumNameThreshold = 50
)

// FleetSummarySchedule 描述一个舰队状态摘要计划：节奏、本地时间、作用域与投递渠道。
//
// Weekday 使用与前端表单一致的 0–6 契约（0 = 周日），仅 weekly 节奏需要设置。
type FleetSummarySchedule struct {
	ID            string     `json:"id"`
	Revision      int        `json:"revision"`
	Name          string     `json:"name"`
	Cadence       string     `json:"cadence"`
	Timezone      string     `json:"timezone"`
	LocalTime     string     `json:"local_time"`
	Weekday       *int       `json:"weekday"`
	NameThreshold int        `json:"name_threshold"`
	Enabled       bool       `json:"enabled"`
	NodeIDs       []string   `json:"node_ids"`
	GroupIDs      []string   `json:"group_ids"`
	ChannelIDs    []string   `json:"channel_ids"`
	NextRunAt     *time.Time `json:"next_run_at"`
	LastRunAt     *time.Time `json:"last_run_at"`
	LastResult    *string    `json:"last_result"`
	DeliveryState *string    `json:"delivery_state"`
	LastPeriodKey string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// FleetSummaryCounts 是调度截止时刻的可用性快照；名称列表按计划阈值独立截断。
type FleetSummaryCounts struct {
	Online       int      `json:"online"`
	Offline      int      `json:"offline"`
	Total        int      `json:"total"`
	OnlineNames  []string `json:"online_names"`
	OfflineNames []string `json:"offline_names"`
}

// FleetSummaryRun 是一次计划执行或手动测试的审计记录。名称与计数来自当时的快照，
// 计划删除后依然保留，但不再引用任何原始指标或心跳数据。
type FleetSummaryRun struct {
	ID           string             `json:"id"`
	ScheduleID   string             `json:"schedule_id"`
	ScheduleName string             `json:"schedule_name"`
	PeriodKey    string             `json:"period_key"`
	Cadence      string             `json:"cadence"`
	Timezone     string             `json:"timezone"`
	Cutoff       time.Time          `json:"cutoff"`
	Kind         string             `json:"kind"`
	Status       string             `json:"status"`
	Counts       FleetSummaryCounts `json:"counts"`
	ChannelCount int                `json:"channel_count"`
	ResultCode   string             `json:"result_code"`
	CreatedAt    time.Time          `json:"created_at"`
	CompletedAt  *time.Time         `json:"completed_at"`
}
