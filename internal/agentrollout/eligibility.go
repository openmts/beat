package agentrollout

import (
	"database/sql"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

type nodeState struct {
	version, os, arch string
	lastSeen          sql.NullTime
}

func (tx *transaction) agent(identity model.AgentIdentity) (nodeState, error) {
	var node nodeState
	var hash []byte
	var revoked, created sql.NullTime
	err := tx.db.QueryRowContext(tx.ctx, "SELECT agent_token_hash,agent_token_created_at,agent_token_revoked_at,agent_version,os,arch,last_seen FROM nodes WHERE id=?", identity.NodeID).Scan(&hash, &created, &revoked, &node.version, &node.os, &node.arch, &node.lastSeen)
	if err != nil || !created.Valid || len(hash) != 32 || revoked.Valid || identity.Mode != model.AgentCredentialActive || model.AgentCredentialFingerprint(hash) != identity.CredentialFingerprint {
		return node, ErrForbidden
	}
	return node, nil
}

func (tx *transaction) eligible(input leaseCandidate, caps protocol.Capabilities) (protocol.Artifact, string, error) {
	artifact, err := platformArtifact(input.release.Manifest, caps)
	if err != nil {
		return artifact, "unsupported_platform", nil
	}
	for _, reason := range []string{capabilityReason(input, caps), sourceVersionReason(input, caps, tx.at), artifactPolicyReason(artifact, input, caps)} {
		if reason != "" {
			return artifact, reason, nil
		}
	}
	if tx.at.Before(input.rollout.Policy.WindowStart) || !tx.at.Before(input.rollout.Policy.WindowEnd) {
		return artifact, "maintenance_window", nil
	}
	var active int
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_task_targets WHERE node_id=? AND state IN('queued','leased','running','cancel_requested','unknown')", input.target.NodeID).Scan(&active); err != nil {
		return artifact, "", ErrUnavailable
	}
	if active > 0 {
		return artifact, "remote_operations_active", nil
	}
	return artifact, "", nil
}

func capabilityReason(input leaseCandidate, caps protocol.Capabilities) string {
	if caps.Manager == "external_manager" {
		return "external_manager"
	}
	managers := map[string]string{"linux": "systemd", "windows": "windows_service", "darwin": "launchd", "freebsd": "rc.d"}
	if managers[caps.OS] != caps.Manager || !caps.UpdateEnabled {
		return "manual_only"
	}
	if caps.OS != input.node.os || !sameArchitecture(caps.Arch, input.node.arch) {
		return "platform_changed"
	}
	return ""
}

func sourceVersionReason(input leaseCandidate, caps protocol.Capabilities, now time.Time) string {
	if input.target.Version == "" || caps.CurrentVersion == "" {
		return "unknown_version"
	}
	if !protocol.SameReleaseVersion(input.node.version, input.target.Version) || !protocol.SameReleaseVersion(caps.CurrentVersion, input.node.version) {
		return "version_changed"
	}
	comparison, valid := compareVersion(input.release.Manifest.Version, input.node.version)
	if !valid || !allowedDirection(input.rollout.Action, comparison) {
		return "downgrade_or_unknown_version"
	}
	if !input.node.lastSeen.Valid || now.Sub(input.node.lastSeen.Time) > time.Duration(input.rollout.Policy.ReportFreshnessSeconds)*time.Second {
		return "stale_heartbeat"
	}
	return ""
}

func artifactPolicyReason(artifact protocol.Artifact, input leaseCandidate, caps protocol.Capabilities) string {
	if caps.ProtocolVersion < input.release.Manifest.MinimumProtocol || caps.ProtocolVersion > input.release.Manifest.MaximumProtocol {
		return "protocol_incompatible"
	}
	if artifact.Support != "supported" && (artifact.Support != "preview" || !input.rollout.Policy.AllowPreview) {
		return "platform_not_supported"
	}
	if caps.AvailableBytes < artifact.Size*requiredArtifactCopies {
		return "insufficient_space"
	}
	return ""
}

func platformArtifact(manifest protocol.Manifest, capabilities protocol.Capabilities) (protocol.Artifact, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.OS == capabilities.OS && artifact.Arch == capabilities.Arch && artifact.Kind != "external_manager" {
			return artifact, nil
		}
	}
	return protocol.Artifact{}, ErrNotFound
}

func allowedDirection(action string, comparison int) bool {
	if action == protocol.AssignmentRollback {
		return comparison < 0
	}
	return comparison > 0
}
