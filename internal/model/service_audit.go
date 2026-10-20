package model

// ServiceAuditEvent 描述服务令牌数据面的一次审计事件。
// 调用方只填写有界字段：禁止写入令牌明文、令牌哈希或会话前缀。
type ServiceAuditEvent struct {
	RequestID     string
	PrincipalID   string
	PrincipalName string
	Action        string
	ResourceType  string
	ResourceID    string
	Outcome       string
	Reason        string
	Scope         string
	Method        string
	Path          string
	IPAddress     string
	UserAgent     string
}
