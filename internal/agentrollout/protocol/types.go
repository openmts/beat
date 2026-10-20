// Package protocol 包含签名发布和升级分配的纯数据契约，不执行更新。
package protocol

import "time"

const ManifestVersion = 1

const AssignmentUpgrade = "upgrade"

const AssignmentRollback = "rollback"

type Artifact struct {
	ID               string `json:"id"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Kind             string `json:"kind"`
	Filename         string `json:"filename"`
	Entrypoint       string `json:"entrypoint"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	SBOMDigest       string `json:"sbom_digest"`
	ProvenanceDigest string `json:"provenance_digest"`
	Support          string `json:"support"`
}

type KeyTransition struct {
	ID        string    `json:"id"`
	PublicKey []byte    `json:"public_key"`
	NotBefore time.Time `json:"not_before"`
	ExpiresAt time.Time `json:"expires_at"`
	Signature []byte    `json:"signature"`
}

type Manifest struct {
	SchemaVersion       int            `json:"schema_version"`
	Version             string         `json:"version"`
	Commit              string         `json:"commit"`
	Channel             string         `json:"channel"`
	BuiltAt             time.Time      `json:"built_at"`
	ExpiresAt           time.Time      `json:"expires_at"`
	MinimumServerSchema int            `json:"minimum_server_schema"`
	MinimumProtocol     int            `json:"minimum_protocol"`
	MaximumProtocol     int            `json:"maximum_protocol"`
	ReleaseNotesDigest  string         `json:"release_notes_digest"`
	SigningKeyID        string         `json:"signing_key_id"`
	Revoked             bool           `json:"revoked"`
	Artifacts           []Artifact     `json:"artifacts"`
	NextKey             *KeyTransition `json:"next_key,omitempty"`
}

type SignedManifest struct {
	Payload        []byte `json:"payload"`
	Signature      []byte `json:"signature"`
	SigstoreBundle []byte `json:"sigstore_bundle"`
}

type Capabilities struct {
	CurrentVersion  string `json:"current_version"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	Manager         string `json:"manager"`
	UpdateEnabled   bool   `json:"update_enabled"`
	AvailableBytes  int64  `json:"available_bytes"`
	ProtocolVersion int    `json:"protocol_version"`
}

type Assignment struct {
	ServerSchema int            `json:"server_schema"`
	Action       string         `json:"action"`
	ID           string         `json:"id"`
	RolloutID    string         `json:"rollout_id"`
	NodeID       string         `json:"node_id"`
	Nonce        string         `json:"nonce"`
	ExpiresAt    time.Time      `json:"expires_at"`
	Artifact     Artifact       `json:"artifact"`
	Manifest     SignedManifest `json:"manifest"`
}

type Observation struct {
	AssignmentID   string    `json:"assignment_id"`
	Nonce          string    `json:"nonce"`
	Version        string    `json:"version"`
	ReportAt       time.Time `json:"report_at"`
	Restarts       int       `json:"restarts"`
	ProtocolErrors int       `json:"protocol_errors"`
	MetricErrors   int       `json:"metric_errors"`
	TaskFailures   int       `json:"task_failures"`
	Outcome        string    `json:"outcome"`
}
