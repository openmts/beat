package model

import "time"

const (
	NodeStatusOnline  = "online"
	NodeStatusOffline = "offline"

	AgentCredentialLegacy  = "legacy"
	AgentCredentialActive  = "active"
	AgentCredentialRevoked = "revoked"
)

type AgentIdentity struct {
	NodeID   string
	NodeName string
	Mode     string
}

type Node struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Alias                string     `json:"alias"`
	GroupID              string     `json:"group_id"`
	Host                 string     `json:"host"`
	Port                 int        `json:"port"`
	Status               string     `json:"status"`
	SSHPublicKey         string     `json:"ssh_public_key"`
	CPUModel             string     `json:"cpu_model"`
	OS                   string     `json:"os"`
	Platform             string     `json:"platform"`
	OSVersion            string     `json:"os_version"`
	Kernel               string     `json:"kernel"`
	Arch                 string     `json:"arch"`
	Virtualization       string     `json:"virtualization"`
	AgentVersion         string     `json:"agent_version"`
	SortOrder            int        `json:"sort_order"`
	Tags                 []string   `json:"tags"`
	IsPublic             bool       `json:"is_public"`
	PublicRemark         string     `json:"public_remark"`
	PrivateRemark        string     `json:"-"`
	AgentTokenHash       []byte     `json:"-"`
	AgentTokenPrefix     string     `json:"-"`
	AgentTokenCreatedAt  *time.Time `json:"-"`
	AgentTokenLastUsedAt *time.Time `json:"-"`
	AgentTokenRevokedAt  *time.Time `json:"-"`
	TrafficLimit         int64      `json:"traffic_limit"`
	TrafficLimitType     string     `json:"traffic_limit_type"`
	TrafficResetDay      int        `json:"traffic_reset_day"`
	LastSeen             time.Time  `json:"last_seen"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (node Node) AgentCredentialStatus() string {
	if node.AgentTokenRevokedAt != nil {
		return AgentCredentialRevoked
	}
	if node.AgentTokenCreatedAt != nil && len(node.AgentTokenHash) != 0 {
		return AgentCredentialActive
	}
	return AgentCredentialLegacy
}

type SystemInfo struct {
	CPUModel       string `json:"cpu_model"`
	OS             string `json:"os"`
	Platform       string `json:"platform"`
	OSVersion      string `json:"os_version"`
	Kernel         string `json:"kernel"`
	Arch           string `json:"arch"`
	Virtualization string `json:"virtualization"`
	AgentVersion   string `json:"agent_version"`
}
