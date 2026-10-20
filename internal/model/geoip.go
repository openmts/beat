package model

import "time"

// LocationPublic 仅包含允许公开的国家级结果。
type LocationPublic struct {
	Scope       string     `json:"scope"`
	CountryCode string     `json:"country_code,omitempty"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

type NodeLocation struct {
	LocationPublic
	NodeID        string     `json:"node_id"`
	Mode          string     `json:"mode"`
	Visibility    string     `json:"visibility"`
	Provider      string     `json:"provider"`
	Revision      string     `json:"revision"`
	Stale         bool       `json:"stale"`
	ErrorCode     string     `json:"error_code,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	NextRefreshAt *time.Time `json:"next_refresh_at,omitempty"`
	HasSource     bool       `json:"has_source"`
}

type LocationWrite struct {
	Mode                 string `json:"mode"`
	Scope                string `json:"scope"`
	CountryCode          string `json:"country_code"`
	Visibility           string `json:"visibility"`
	ManagedTarget        string `json:"managed_target,omitempty"`
	Purge                bool   `json:"purge"`
	ConfirmManagedTarget bool   `json:"confirm_managed_target"`
}

type GeoIPSettings struct {
	Provider      string       `json:"provider"`
	Revision      int64        `json:"revision"`
	RemoteConsent bool         `json:"remote_consent"`
	HasCredential bool         `json:"has_credential"`
	MMDB          MMDBMetadata `json:"mmdb"`
}

type GeoIPSettingsWrite struct {
	Provider         string  `json:"provider"`
	RemoteConsent    bool    `json:"remote_consent"`
	Credential       *string `json:"credential,omitempty"`
	ExpectedRevision int64   `json:"expected_revision"`
	Purge            bool    `json:"purge"`
}

type MMDBMetadata struct {
	Edition    string `json:"edition"`
	Source     string `json:"source"`
	BuildEpoch uint64 `json:"build_epoch"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	State      string `json:"state"`
	ErrorCode  string `json:"error_code,omitempty"`
}
