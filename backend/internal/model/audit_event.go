package model

import "time"

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeFailure = "failure"
)

type AdminAuditEvent struct {
	ID            string    `json:"id"`
	RequestID     string    `json:"request_id"`
	ActorID       string    `json:"actor_id"`
	ActorUsername string    `json:"actor_username"`
	Action        string    `json:"action"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id"`
	Outcome       string    `json:"outcome"`
	DetailJSON    string    `json:"detail_json"`
	IPAddress     string    `json:"ip_address"`
	UserAgent     string    `json:"user_agent"`
	SessionPrefix string    `json:"session_prefix"`
	CreatedAt     time.Time `json:"created_at"`
}

type AuditFilter struct {
	Action  string
	ActorID string
	Limit   int
	Offset  int
}

type AuditPage struct {
	Events []AdminAuditEvent `json:"events"`
	Total  int               `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}
