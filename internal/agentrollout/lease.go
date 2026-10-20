package agentrollout

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

const assignmentLifetime = 15 * time.Minute

type leaseCandidate struct {
	target   Target
	rollout  Rollout
	release  Release
	signed   protocol.SignedManifest
	node     nodeState
	artifact protocol.Artifact
}

func (s *Service) agentMutation(ctx context.Context, identity model.AgentIdentity, action func(*transaction, nodeState) error) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	db, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, at: s.now(), username: "agent"}
	node, err := tx.agent(identity)
	if err != nil {
		return err
	}
	if err := action(tx, node); err != nil {
		return err
	}
	tx.at = s.now()
	if _, err := tx.agent(identity); err != nil {
		return err
	}
	if err := db.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) Lease(ctx context.Context, identity model.AgentIdentity, capabilities protocol.Capabilities) (*protocol.Assignment, error) {
	var candidate *leaseCandidate
	err := s.agentMutation(ctx, identity, func(tx *transaction, node nodeState) error {
		var err error
		candidate, err = s.candidate(tx, leaseQuery{identity, node, capabilities})
		return err
	})
	if err != nil || candidate == nil {
		return nil, err
	}
	if err := s.checkArtifact(candidate.artifact); err != nil {
		holdErr := s.agentMutation(ctx, identity, func(tx *transaction, _ nodeState) error {
			if err := tx.exec("UPDATE agent_release_artifacts SET state='missing',updated_at=? WHERE release_id=? AND artifact_id=?", tx.at, candidate.release.ID, candidate.artifact.ID); err != nil {
				return err
			}
			return tx.hold(candidate.target, "artifact_missing")
		})
		return nil, holdErr
	}
	var output *protocol.Assignment
	err = s.agentMutation(ctx, identity, func(tx *transaction, node nodeState) error {
		current, err := s.candidate(tx, leaseQuery{identity, node, capabilities})
		if err != nil {
			return err
		}
		if current == nil || current.target.ID != candidate.target.ID {
			return ErrConflict
		}
		output, err = tx.offer(*current)
		if err != nil {
			return err
		}
		output.ServerSchema = s.schema
		tx.at = s.now()
		_, _, err = s.releaseInTransaction(tx, current.release.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

type leaseQuery struct {
	identity     model.AgentIdentity
	node         nodeState
	capabilities protocol.Capabilities
}

func (s *Service) candidate(tx *transaction, query leaseQuery) (*leaseCandidate, error) {
	identity, node, capabilities := query.identity, query.node, query.capabilities
	var candidate leaseCandidate
	var rolloutID string
	err := tx.db.QueryRowContext(tx.ctx, `SELECT t.id,t.node_snapshot_id,t.node_name,t.source_version,t.os,t.arch,t.wave,t.rollout_id FROM agent_rollout_targets t JOIN agent_rollouts r ON r.id=t.rollout_id WHERE t.node_id=? AND r.state='active' AND r.current_wave=t.wave AND t.state IN('pending','held') ORDER BY r.created_at,t.id LIMIT 1`, identity.NodeID).Scan(&candidate.target.ID, &candidate.target.NodeID, &candidate.target.Name, &candidate.target.Version, &candidate.target.OS, &candidate.target.Arch, &candidate.target.Wave, &rolloutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	candidate.rollout, err = tx.rollout(rolloutID)
	if err != nil {
		return nil, err
	}
	candidate.release, candidate.signed, err = s.releaseInTransaction(tx, candidate.rollout.ReleaseID)
	if err != nil {
		return nil, err
	}
	candidate.node = node
	artifact, reason, err := tx.eligible(candidate, capabilities)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, tx.hold(candidate.target, reason)
	}
	candidate.artifact = artifact
	ready, err := tx.assignmentCapacity(candidate)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, nil
	}
	return &candidate, nil
}

func (tx *transaction) assignmentCapacity(candidate leaseCandidate) (bool, error) {
	var state string
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT state FROM agent_release_artifacts WHERE release_id=? AND artifact_id=?", candidate.release.ID, candidate.artifact.ID).Scan(&state); err != nil {
		return false, ErrUnavailable
	}
	if state != "ready" {
		return false, tx.hold(candidate.target, "artifact_missing")
	}
	var count, own int
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*),COALESCE(SUM(CASE WHEN node_id=? THEN 1 ELSE 0 END),0) FROM agent_rollout_targets WHERE (rollout_id=? OR node_id=?) AND state IN('offered','observing')", candidate.target.NodeID, candidate.rollout.ID, candidate.target.NodeID).Scan(&count, &own); err != nil {
		return false, ErrUnavailable
	}
	if own > 0 {
		return false, tx.hold(candidate.target, "node_update_active")
	}
	if count >= candidate.rollout.Policy.MaxConcurrent {
		return false, tx.hold(candidate.target, "assignment_capacity")
	}
	return true, nil
}

func (tx *transaction) offer(candidate leaseCandidate) (*protocol.Assignment, error) {
	nonce := make([]byte, assignmentNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrUnavailable
	}
	text := hex.EncodeToString(nonce)
	hash := sha256.Sum256([]byte(text))
	id := uuid.NewString()
	expiry := tx.at.Add(assignmentLifetime + time.Duration(candidate.rollout.Policy.ObservationSeconds)*time.Second)
	if candidate.release.Manifest.ExpiresAt.Before(expiry) {
		expiry = candidate.release.Manifest.ExpiresAt
	}
	if candidate.rollout.Policy.WindowEnd.Before(expiry) {
		expiry = candidate.rollout.Policy.WindowEnd
	}
	if err := tx.exec("INSERT INTO agent_rollout_attempts(id,target_id,nonce_hash,state,created_at) VALUES(?,?,?,'offered',?)", id, candidate.target.ID, hash[:], tx.at); err != nil {
		return nil, err
	}
	result, err := tx.db.ExecContext(tx.ctx, `UPDATE agent_rollout_targets SET state='offered',reason='',current_attempt_id=?,artifact_id=?,nonce_hash=?,lease_expires_at=?,healthy_since=NULL,last_report_at=NULL,observation_json='{}',updated_at=? WHERE id=? AND state IN('pending','held')`, id, candidate.artifact.ID, hash[:], expiry, tx.at, candidate.target.ID)
	if err := requireOne(result, err); err != nil {
		return nil, err
	}
	if err := tx.targetEvent(candidate.target.ID, "assignment.offered", ""); err != nil {
		return nil, err
	}
	return &protocol.Assignment{Action: candidate.rollout.Action, ID: id, RolloutID: candidate.rollout.ID, NodeID: candidate.target.NodeID, Nonce: text, ExpiresAt: expiry, Artifact: candidate.artifact, Manifest: candidate.signed}, nil
}

func (tx *transaction) hold(target Target, reason string) error {
	result, err := tx.db.ExecContext(tx.ctx, "UPDATE agent_rollout_targets SET state='held',reason=?,updated_at=? WHERE id=? AND state IN('pending','held') AND (state<>'held' OR reason<>?)", reason, tx.at, target.ID, reason)
	if err != nil {
		return ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if count == 0 {
		return nil
	}
	return tx.targetEvent(target.ID, "assignment.held", reason)
}

func (tx *transaction) targetEvent(id, kind, reason string) error {
	result, err := tx.db.ExecContext(tx.ctx, `INSERT INTO agent_rollout_events(id,rollout_id,target_id,kind,reason,created_at) SELECT ?,rollout_id,id,?,?,? FROM agent_rollout_targets WHERE id=?`, uuid.NewString(), kind, reason, tx.at, id)
	return requireOne(result, err)
}
