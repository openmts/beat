package model

import "time"

const (
	TrafficReportDaily   = "daily"
	TrafficReportWeekly  = "weekly"
	TrafficReportMonthly = "monthly"

	TrafficReportDeliverySuccess = "success"
	TrafficReportDeliveryPartial = "partial"
	TrafficReportDeliveryFailed  = "failed"
)

type TrafficReportSchedule struct {
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Cadence       string                       `json:"cadence"`
	Timezone      string                       `json:"timezone"`
	SendHour      int                          `json:"send_hour"`
	SendMinute    int                          `json:"send_minute"`
	Weekday       int                          `json:"weekday"`
	MonthDay      int                          `json:"month_day"`
	AllNodes      bool                         `json:"all_nodes"`
	NodeIDs       []string                     `json:"node_ids"`
	AllChannels   bool                         `json:"all_channels"`
	ChannelIDs    []string                     `json:"channel_ids"`
	Enabled       bool                         `json:"enabled"`
	LastRunAt     *time.Time                   `json:"last_run_at"`
	NextRunAt     time.Time                    `json:"next_run_at"`
	LastDelivery  *TrafficReportDeliveryStatus `json:"last_delivery,omitempty"`
	LastPeriodKey string                       `json:"-"`
	CreatedAt     time.Time                    `json:"created_at"`
	UpdatedAt     time.Time                    `json:"updated_at"`
}

type TrafficReportDeliveryStatus struct {
	State       string    `json:"state"`
	Message     string    `json:"message"`
	Delivered   int       `json:"delivered"`
	Total       int       `json:"total"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type TrafficReportPeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Key   string    `json:"key"`
}

type TrafficReportNode struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Alias     string  `json:"alias"`
	Sent      float64 `json:"sent"`
	Received  float64 `json:"received"`
	Used      float64 `json:"used"`
	LimitType string  `json:"limit_type"`
}

type TrafficReport struct {
	ScheduleID   string              `json:"schedule_id"`
	ScheduleName string              `json:"schedule_name"`
	Cadence      string              `json:"cadence"`
	Timezone     string              `json:"timezone"`
	Period       TrafficReportPeriod `json:"period"`
	GeneratedAt  time.Time           `json:"generated_at"`
	Nodes        []TrafficReportNode `json:"nodes"`
}

type TrafficReportRunResult struct {
	Report   TrafficReport               `json:"report"`
	Delivery TrafficReportDeliveryStatus `json:"delivery"`
}
