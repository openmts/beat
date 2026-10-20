package handler

import (
	"net/http"
	"strings"

	"github.com/beat/backend/internal/agentenrollment"
)

// agentDiscoveryProtocolVersion 是挑战响应里回显的协议版本。
const agentDiscoveryProtocolVersion = agentenrollment.ProtocolVersion

// agentDiscoveryMaxBodyBytes 限制入网协议请求体，避免匿名接口被大包拖垮。
const agentDiscoveryMaxBodyBytes = 32 << 10

// agentChallengeRequest 是申请挑战的请求体。
type agentChallengeRequest struct {
	PublicKey      string `json:"public_key"`
	MetadataDigest string `json:"metadata_digest"`
}

// agentChallengeResponse 返回一次性挑战、服务端规范源与协议版本。
type agentChallengeResponse struct {
	ChallengeID     string `json:"challenge_id"`
	Challenge       string `json:"challenge"`
	Origin          string `json:"origin"`
	IssuedAt        string `json:"issued_at"`
	ExpiresAt       string `json:"expires_at"`
	ProtocolVersion string `json:"protocol_version"`
}

// agentMetadataPayload 是 Agent 自报的环境信息，全部为展示建议。
type agentMetadataPayload struct {
	Hostname          string `json:"hostname"`
	AdvertisedHost    string `json:"advertised_host"`
	OS                string `json:"os"`
	Architecture      string `json:"architecture"`
	AgentVersion      string `json:"agent_version"`
	Virtualization    string `json:"virtualization"`
	SSHPort           int    `json:"ssh_port"`
	InstallationNonce string `json:"installation_nonce"`
}

// agentRequestCreatePayload 是创建或恢复入网请求的请求体。
type agentRequestCreatePayload struct {
	ChallengeID    string               `json:"challenge_id"`
	Challenge      string               `json:"challenge"`
	PublicKey      string               `json:"public_key"`
	MetadataDigest string               `json:"metadata_digest"`
	Metadata       agentMetadataPayload `json:"metadata"`
	Signature      string               `json:"signature"`
}

// agentRequestCreateResponse 交付一次性请求令牌，令牌明文只出现一次。
type agentRequestCreateResponse struct {
	RequestID    string `json:"request_id"`
	RequestToken string `json:"request_token"`
	Status       string `json:"status"`
	GrantName    string `json:"grant_name"`
}

// agentStatusPayload 是轮询状态时提交的签名材料。
type agentStatusPayload struct {
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

// agentStatusResponse 只暴露该请求自身的状态与有界原因码。
type agentStatusResponse struct {
	RequestID        string `json:"request_id"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
	Ticket           string `json:"ticket,omitempty"`
	TicketGeneration int    `json:"ticket_generation,omitempty"`
	TicketExpiresAt  string `json:"ticket_expires_at,omitempty"`
	NodeID           string `json:"node_id,omitempty"`
	NodeName         string `json:"node_name,omitempty"`
}

// agentClaimPayload 是兑换节点凭据时提交的票据与签名材料。
type agentClaimPayload struct {
	Ticket    string `json:"ticket"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

// agentClaimResponse 一次性交付节点令牌与完整 Agent 配置。
type agentClaimResponse struct {
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name"`
	GroupID    string `json:"group_id"`
	ServerURL  string `json:"server_url"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	AgentToken string `json:"agent_token"`
	// ReportInterval 与一次性 join 流程共用同一默认值，避免 Agent 侧出现两套节奏。
	ReportInterval string `json:"report_interval"`
	Recovered      bool   `json:"recovered"`
}

// enrollmentGrantPayload 是管理端看到的授权视图，秘密只以展示前缀出现。
type enrollmentGrantPayload struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	SecretPrefix        string   `json:"secret_prefix"`
	Mode                string   `json:"mode"`
	Enabled             bool     `json:"enabled"`
	ExpiresAt           string   `json:"expires_at"`
	RevokedAt           string   `json:"revoked_at,omitempty"`
	DefaultGroupID      string   `json:"default_group_id"`
	DefaultGroupName    string   `json:"default_group_name"`
	NodeNamePrefix      string   `json:"node_name_prefix"`
	Tags                []string `json:"tags"`
	Remarks             string   `json:"remarks"`
	Public              bool     `json:"public"`
	SSHPort             int      `json:"ssh_port"`
	SourceIPAsHost      bool     `json:"source_ip_as_host"`
	SourceCIDRs         []string `json:"source_cidrs"`
	HostnamePattern     string   `json:"hostname_pattern"`
	ClaimRecovery       bool     `json:"claim_recovery"`
	MaximumEnrollments  int      `json:"maximum_enrollments"`
	ConsumedEnrollments int      `json:"consumed_enrollments"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
}

// enrollmentGrantListResponse 是授权列表信封。
type enrollmentGrantListResponse struct {
	Grants []enrollmentGrantPayload `json:"grants"`
}

// enrollmentGrantSecretResponse 在创建或轮换后一次性返回授权秘密明文。
type enrollmentGrantSecretResponse struct {
	Grant  enrollmentGrantPayload `json:"grant"`
	Secret string                 `json:"secret"`
}

// enrollmentGrantUpsertPayload 是创建或覆盖授权时提交的策略。
type enrollmentGrantUpsertPayload struct {
	Name               string   `json:"name"`
	Mode               string   `json:"mode"`
	Enabled            bool     `json:"enabled"`
	ExpiresAt          string   `json:"expires_at"`
	DefaultGroupID     string   `json:"default_group_id"`
	NodeNamePrefix     string   `json:"node_name_prefix"`
	Tags               []string `json:"tags"`
	Remarks            string   `json:"remarks"`
	Public             bool     `json:"public"`
	SSHPort            int      `json:"ssh_port"`
	UseSourceIPAsHost  bool     `json:"source_ip_as_host"`
	SourceCIDRs        []string `json:"source_cidrs"`
	HostnamePattern    string   `json:"hostname_pattern"`
	ClaimRecovery      bool     `json:"claim_recovery"`
	MaximumEnrollments int      `json:"maximum_enrollments"`
}

// enrollmentRequestPayload 是管理端看到的请求视图；节点名始终是可读名称。
type enrollmentRequestPayload struct {
	ID                string `json:"id"`
	GrantID           string `json:"grant_id"`
	GrantName         string `json:"grant_name"`
	Status            string `json:"status"`
	NodeName          string `json:"node_name"`
	ReservedNodeID    string `json:"reserved_node_id,omitempty"`
	Hostname          string `json:"hostname"`
	AdvertisedHost    string `json:"advertised_host"`
	OS                string `json:"os"`
	Architecture      string `json:"architecture"`
	AgentVersion      string `json:"agent_version"`
	Virtualization    string `json:"virtualization"`
	SSHPort           int    `json:"ssh_port"`
	SourceIP          string `json:"source_ip"`
	InstallationNonce string `json:"installation_nonce"`
	FirstRequestAt    string `json:"first_request_at"`
	LastRequestAt     string `json:"last_request_at"`
	ExpiresAt         string `json:"expires_at"`
	Reason            string `json:"reason,omitempty"`
	ApprovedBy        string `json:"approved_by,omitempty"`
	RejectedBy        string `json:"rejected_by,omitempty"`
	ClaimedAt         string `json:"claimed_at,omitempty"`
}

// enrollmentRequestListResponse 是请求列表信封，游标为空表示没有下一页。
type enrollmentRequestListResponse struct {
	Requests   []enrollmentRequestPayload `json:"requests"`
	NextCursor string                     `json:"next_cursor"`
}

// enrollmentApprovalPayload 是人工审批提交的最终节点字段。
type enrollmentApprovalPayload struct {
	NodeName string   `json:"node_name"`
	GroupID  string   `json:"group_id"`
	Tags     []string `json:"tags"`
	Remarks  string   `json:"remarks"`
	Public   bool     `json:"public"`
	Host     string   `json:"host"`
	SSHPort  int      `json:"ssh_port"`
}

// enrollmentRejectionPayload 是拒绝请求时提交的原因码。
type enrollmentRejectionPayload struct {
	Reason string `json:"reason"`
}

// enrollmentGrantSecret 从 bearer 头取出授权或请求秘密。
func enrollmentGrantSecret(request *http.Request) (string, bool) {
	return enrollmentBearerSecret(request)
}

func enrollmentBearerSecret(request *http.Request) (string, bool) {
	header := strings.TrimSpace(request.Header.Get("Authorization"))
	const scheme = "Bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	secret := strings.TrimSpace(header[len(scheme):])
	return secret, secret != ""
}
