package model

import (
	"encoding/json"
	"time"
)

type NotificationDeliveryRef struct {
	ID              string `json:"id"`
	ChannelID       string `json:"channel_id"`
	State           string `json:"state"`
	ChannelRevision int64  `json:"channel_revision"`
	AttemptCount    int    `json:"attempt_count"`
}

type NotificationChannel struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	ChannelType     string                    `json:"channel_type"`
	Enabled         bool                      `json:"enabled"`
	CurrentRevision int64                     `json:"current_revision"`
	EventKinds      []string                  `json:"event_kinds"`
	Config          json.RawMessage           `json:"config"`
	SecretPresent   map[string]bool           `json:"secret_present"`
	TemplateSubject string                    `json:"template_subject"`
	TemplateText    string                    `json:"template_text"`
	Health          NotificationChannelHealth `json:"health"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

type NotificationChannelWrite struct {
	Name             string          `json:"name"`
	ChannelType      string          `json:"channel_type"`
	Enabled          bool            `json:"enabled"`
	ExpectedRevision int64           `json:"expected_revision"`
	EventKinds       []string        `json:"event_kinds"`
	Config           json.RawMessage `json:"config"`
	ClearSecrets     []string        `json:"clear_secrets"`
	TemplateSubject  string          `json:"template_subject"`
	TemplateText     string          `json:"template_text"`
}

type NotificationChannelHealth struct {
	Revision            int64      `json:"revision"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	Circuit             string     `json:"circuit"`
	OpenUntil           *time.Time `json:"open_until,omitempty"`
	LastSuccess         *time.Time `json:"last_success,omitempty"`
	LastFailure         *time.Time `json:"last_failure,omitempty"`
	ErrorCode           string     `json:"error_code,omitempty"`
	ErrorStage          string     `json:"error_stage,omitempty"`
	QueueDepth          int64      `json:"queue_depth"`
}

type NotificationPayload struct {
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	Data    map[string]string `json:"data"`
}

type NotificationDispatch struct {
	DeliveryID, ChannelID, ChannelType string
	Revision                           int64
	Attempt                            int
	Config                             json.RawMessage `json:"-"`
	TemplateSubject, TemplateText      string
	Payload                            NotificationPayload      `json:"-"`
	Policy                             NotificationEgressPolicy `json:"-"`
}

func (NotificationDispatch) String() string { return "notification dispatch" }

func (NotificationDispatch) GoString() string { return "notification dispatch" }

type NotificationSendResult struct {
	Success, Retryable            bool
	RetryAfter                    time.Duration
	ErrorCode, Stage, StatusClass string
}

type NotificationDelivery struct {
	NotificationDeliveryRef
	MessageID     string                `json:"message_id"`
	ChannelName   string                `json:"channel_name"`
	ChannelType   string                `json:"channel_type"`
	EventKind     string                `json:"event_kind"`
	SourceKind    string                `json:"source_kind"`
	SourceID      string                `json:"source_id"`
	NextAttemptAt time.Time             `json:"next_attempt_at"`
	ErrorCode     string                `json:"error_code,omitempty"`
	ErrorStage    string                `json:"error_stage,omitempty"`
	StatusClass   string                `json:"status_class,omitempty"`
	RetryOf       string                `json:"retry_of,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	CompletedAt   *time.Time            `json:"completed_at,omitempty"`
	Attempts      []NotificationAttempt `json:"attempts,omitempty"`
}

type NotificationAttempt struct {
	Number      int        `json:"number"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMS  int64      `json:"duration_ms"`
	Outcome     string     `json:"outcome"`
	ErrorCode   string     `json:"error_code,omitempty"`
	ErrorStage  string     `json:"error_stage,omitempty"`
	StatusClass string     `json:"status_class,omitempty"`
}

type NotificationDeliveryPage struct {
	Items  []NotificationDelivery `json:"items"`
	Total  int64                  `json:"total"`
	Offset int                    `json:"offset"`
	Limit  int                    `json:"limit"`
}

type NotificationDeliveryCounts struct {
	Pending   int `json:"pending"`
	Leased    int `json:"leased"`
	Succeeded int `json:"succeeded"`
	Dead      int `json:"dead"`
	Cancelled int `json:"cancelled"`
}

type NotificationEgressPolicy struct {
	Revision   int64                         `json:"revision"`
	Exceptions []NotificationEgressException `json:"exceptions"`
}

type NotificationEgressException struct {
	Host      string    `json:"host,omitempty"`
	Port      int       `json:"port"`
	CIDR      string    `json:"cidr,omitempty"`
	Purpose   string    `json:"purpose"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type NotificationSettings struct {
	Revision             int64                    `json:"revision"`
	SuccessRetentionDays int                      `json:"success_retention_days"`
	DeadRetentionDays    int                      `json:"dead_retention_days"`
	MaximumWorkers       int                      `json:"maximum_workers"`
	EgressPolicy         NotificationEgressPolicy `json:"egress_policy"`
}

type NotificationSettingsWrite struct {
	ExpectedRevision     int64                    `json:"expected_revision"`
	SuccessRetentionDays int                      `json:"success_retention_days"`
	DeadRetentionDays    int                      `json:"dead_retention_days"`
	MaximumWorkers       int                      `json:"maximum_workers"`
	EgressPolicy         NotificationEgressPolicy `json:"egress_policy"`
}
