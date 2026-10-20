package remoteoperations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

type dispatch struct {
	nodeID, targetID, taskID, policyID, capability, ignoreAttempt string
	payload                                                       []byte
	lease                                                         Lease
}

func (service *Service) Lease(ctx context.Context, request LeaseRequest) (*Lease, error) {
	var leased *Lease
	err := service.write(ctx, func(tx transaction) error {
		nodeID, err := tx.node(request.Proof)
		if err != nil {
			return err
		}
		local, digest, err := tx.capability(nodeID)
		if err != nil {
			return err
		}
		if digest != request.CapabilityDigest {
			return ErrConflict
		}
		if err := tx.reconcile(); err != nil {
			return err
		}
		item, err := tx.nextDispatch(nodeID, service.spool != nil)
		if err != nil || item == nil {
			return err
		}
		item.capability = digest
		if err := tx.validateDispatch(item, local); err != nil {
			if err == ErrBusy {
				return nil
			}
			return err
		}
		if err := tx.leaseDispatch(item); err != nil {
			return err
		}
		item.lease.Payload.NodeIDs = nil
		item.lease.Payload.GroupIDs = nil
		item.lease.Payload.PolicyID = ""
		item.lease.Payload.DedupeKey = ""
		leased = &item.lease
		return nil
	})
	return leased, err
}

func (tx transaction) nextDispatch(nodeID string, files bool) (*dispatch, error) {
	item := &dispatch{nodeID: nodeID}
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT n.id,t.id,t.policy_id,t.payload_encrypted FROM operation_task_targets n JOIN operation_tasks t ON t.id=n.task_id
 JOIN operation_policies p ON p.id=t.policy_id JOIN operation_capabilities c ON c.node_id=n.node_id LEFT JOIN operation_transfers f ON f.task_id=t.id LEFT JOIN operation_artifacts a ON a.id=f.artifact_id
 WHERE n.node_id=? AND n.state='queued' AND t.cancel_requested=0 AND t.expires_at>? AND (? OR t.kind NOT IN ('deploy','collect')) AND (t.kind NOT IN ('deploy','collect') OR (a.expires_at>? AND ((t.kind='deploy' AND a.state='ready' AND ((f.require_scan=0 AND COALESCE(json_extract(p.configuration_json,'$.require_scan'),0)=0) OR a.scan_status='clean')) OR (t.kind='collect' AND a.state IN ('ready','uploading','awaiting_source')))))`+dispatchAdmissionSQL+` ORDER BY t.created_at,n.id LIMIT 1`, nodeID, tx.now.UnixMilli(), files, tx.now.UnixMilli()).Scan(&item.targetID, &item.taskID, &item.policyID, &item.payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	return item, nil
}

func (tx transaction) validateDispatch(item *dispatch, local Policy) error {
	plain, err := tx.access.Open(item.payload, secretbox.OperationTaskAAD(item.taskID))
	if err != nil {
		return ErrUnavailable
	}
	defer clear(plain)
	if json.Unmarshal(plain, &item.lease.Payload) != nil {
		return ErrUnavailable
	}
	policy, err := tx.policy(item.policyID)
	if err != nil {
		return err
	}
	var count int
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_task_targets WHERE task_id=?", item.taskID).Scan(&count); err != nil {
		return ErrUnavailable
	}
	limits := item.lease.Payload.Limits
	limits.Targets = count
	if err := tx.scope(item.policyID, item.nodeID); err != nil {
		return err
	}
	if limits.Kind == "shell" || limits.Kind == "snippet" {
		if err := AuthorizeShell(policy, local, item.lease.Payload.ShellFamily); err != nil {
			return err
		}
	}
	if err := Authorize(policy, local, limits); err != nil {
		return err
	}
	if item.lease.Payload.File != nil {
		if err := tx.fileDispatchAllowed(item.taskID); err != nil {
			return err
		}
		if err := AuthorizeFile(policy, local, FileRequest{Direction: limits.Kind, File: *item.lease.Payload.File}); err != nil {
			return err
		}
	}
	if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM operation_task_targets WHERE node_id=? AND current_attempt<>? AND state IN ('leased','running','unknown','cancel_requested')`, item.nodeID, item.ignoreAttempt).Scan(&count); err != nil {
		return ErrUnavailable
	}
	if count >= min(policy.Concurrency, local.Concurrency) {
		return ErrBusy
	}
	return nil
}

func (tx transaction) leaseDispatch(item *dispatch) error {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrUnavailable
	}
	digest := sha256.Sum256(nonce[:])
	item.lease.AttemptID = uuid.NewString()
	item.lease.Nonce = hex.EncodeToString(nonce[:])
	item.lease.ExpiresAt = tx.now.Add(LeaseDuration).UnixMilli()
	if err := tx.one(`INSERT INTO operation_attempts(id,target_id,attempt_number,nonce_hash,state,capability_digest,lease_expires_at,created_at,retry_actor,retry_reason)
		VALUES(?,?,(SELECT COALESCE(MAX(attempt_number),0)+1 FROM operation_attempts WHERE target_id=?),?,'leased',?,?,?,(SELECT retry_actor FROM operation_task_targets WHERE id=?),(SELECT retry_reason FROM operation_task_targets WHERE id=?))`,
		item.lease.AttemptID, item.targetID, item.targetID, digest[:], item.capability, item.lease.ExpiresAt, tx.now.UnixMilli(), item.targetID, item.targetID); err != nil {
		return err
	}
	return tx.one("UPDATE operation_task_targets SET current_attempt=?,state='leased',retry_actor='',retry_reason='',updated_at=? WHERE id=?", item.lease.AttemptID, tx.now.UnixMilli(), item.targetID)
}

func (tx transaction) reconcile() error {
	if err := tx.exec(`UPDATE operation_attempts SET state='expired' WHERE state='leased' AND started_at IS NULL AND lease_expires_at<=?`, tx.now.UnixMilli()); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE operation_task_targets SET state='queued',current_attempt='',updated_at=? WHERE state='leased' AND current_attempt IN
		(SELECT id FROM operation_attempts WHERE state='expired' AND started_at IS NULL)`, tx.now.UnixMilli()); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE operation_attempts SET state='unknown' WHERE state='running' AND heartbeat_at<?`, tx.now.Add(-HeartbeatGrace).UnixMilli()); err != nil {
		return err
	}
	return tx.exec(`UPDATE operation_task_targets SET state='unknown',updated_at=? WHERE state='running' AND current_attempt IN (SELECT id FROM operation_attempts WHERE state='unknown')`, tx.now.UnixMilli())
}
