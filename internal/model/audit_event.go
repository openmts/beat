package model

import "time"

const (
	AuditOutcomeSuccess = "success"
	AuditOutcomeFailure = "failure"
	AuditActorAdmin     = "admin"
	AuditActorService   = "service"
)

type AdminAuditEvent struct {
	ID            string    `json:"id"`
	RequestID     string    `json:"request_id"`
	ActorType     string    `json:"actor_type"`
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

// AuditFilter 是服务端筛选与游标分页的输入；offset 遍历已被不透明游标取代。
type AuditFilter struct {
	Action  string
	ActorID string
	Cursor  string
	Limit   int
}

// AuditPage 只返回一页审计事件与下一页游标，不再回传总数与偏移。
type AuditPage struct {
	Events     []AdminAuditEvent `json:"events"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
