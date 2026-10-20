package agentupdate

import (
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

type verifiedAssignment struct {
	assignment protocol.Assignment
	manifest   protocol.Manifest
	verifiedAt time.Time
}

func (u *Updater) verify(assignment protocol.Assignment, at time.Time) (verifiedAssignment, error) {
	value := verifiedAssignment{assignment: assignment, verifiedAt: at}
	if !validAssignmentIdentity(assignment, u.config.NodeID, at) {
		return value, ErrAssignment
	}
	decoded, err := protocol.DecodeManifest(assignment.Manifest.Payload)
	if err != nil {
		return value, err
	}
	trust, ok := u.signingTrust(decoded.SigningKeyID)
	if !ok {
		return value, protocol.ErrSignature
	}
	manifest, err := protocol.VerifyManifestSignature(assignment.Manifest, trust, at)
	if err != nil {
		return value, err
	}
	if assignment.ServerSchema < manifest.MinimumServerSchema {
		return value, ErrAssignment
	}
	if err = u.compatibleArtifact(manifest, assignment.Artifact); err != nil {
		return value, err
	}
	if !validDirection(assignment.Action, manifest.Version, u.config.Installed.Version) {
		return value, ErrVersion
	}
	value.manifest = manifest
	// Agent核心只保留离线Ed25519所需证明，不执行或存储Sigstore进程输入。
	value.assignment.Manifest.SigstoreBundle = nil
	return value, nil
}

func validAssignmentIdentity(assignment protocol.Assignment, nodeID string, now time.Time) bool {
	return (assignment.Action == protocol.AssignmentUpgrade || assignment.Action == protocol.AssignmentRollback) && assignment.NodeID == nodeID && protocol.ValidSigningKeyID(assignment.ID) && protocol.ValidSigningKeyID(assignment.RolloutID) && protocol.ValidSHA256(assignment.Nonce) && assignment.ExpiresAt.After(now) && !assignment.ExpiresAt.After(now.Add(MaximumAssignmentLifetime))
}

func (u *Updater) signingTrust(id string) (protocol.SigningTrust, bool) {
	var found protocol.SigningTrust
	count := 0
	for _, key := range u.config.Keys {
		if key.KeyID == id {
			found = key
			count++
		}
	}
	return found, count == 1
}

func (u *Updater) compatibleArtifact(manifest protocol.Manifest, artifact protocol.Artifact) error {
	if artifact.OS != u.config.OS || artifact.Arch != u.config.Arch || artifact.Kind != "binary" {
		return ErrManualOnly
	}
	if artifact.Support != "supported" && artifact.Support != "preview" {
		return ErrManualOnly
	}
	if artifact.Entrypoint != filepath.Base(u.config.Executable) {
		return ErrAssignmentEntrypoint
	}
	if manifest.MinimumProtocol > u.config.AgentProtocol || manifest.MaximumProtocol < u.config.AgentProtocol {
		return ErrAssignmentProtocol
	}
	return manifestContains(manifest, artifact)
}

func manifestContains(manifest protocol.Manifest, artifact protocol.Artifact) error {
	for _, candidate := range manifest.Artifacts {
		if candidate.ID == artifact.ID {
			if candidate != artifact {
				return ErrAssignment
			}
			return nil
		}
	}
	return ErrAssignmentArtifact
}

func validDirection(action, target, current string) bool {
	order, valid := protocol.CompareVersions(target, current)
	return valid && ((action == protocol.AssignmentUpgrade && order > 0) || (action == protocol.AssignmentRollback && order < 0))
}
