package agentjoin

import "time"

const (
	TokenPrefix                     = "beat_join_v1_"
	TokenLifetime                   = 30 * time.Minute
	DefaultReportInterval           = "5s"
	MaximumBodyBytes                = 16 << 10
	MaximumNodeNameBytes            = 100
	MaximumHostBytes                = 253
	MaximumRequestIDBytes           = 128
	MaximumServerURLBytes           = 2048
	NodeNameConflictMessage         = "node name already exists; rerun join with --name <unique-name>"
	AgentIdentityUnavailableMessage = "agent identity is no longer active; retain the saved identity and contact an administrator"
)

type IssueRequest struct {
	ServerURL string `json:"server_url"`
	AllowHTTP bool   `json:"allow_http,omitempty"`
}

type IssueResult struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ServerURL string    `json:"server_url"`
	ExpiresAt time.Time `json:"expires_at"`
	Command   string    `json:"command"`
}

type Request struct {
	RequestID      string `json:"request_id"`
	AgentToken     string `json:"agent_token"`
	NodeName       string `json:"node_name"`
	AdvertisedHost string `json:"advertised_host,omitempty"`
	SSHPort        int    `json:"ssh_port"`
}

type Config struct {
	ServerURL      string `json:"server_url"`
	AgentToken     string `json:"agent_token"`
	NodeName       string `json:"node_name"`
	AdvertisedHost string `json:"advertised_host"`
	SSHPort        int    `json:"ssh_port"`
	ReportInterval string `json:"report_interval"`
}

type Result struct {
	NodeID string `json:"node_id"`
	Config Config `json:"config"`
}

type Envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}
