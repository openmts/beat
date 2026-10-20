package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/beat/backend/internal/notificationqueue"
)

type ForceNodeDelete struct {
	NodeID                 string `json:"-"`
	Reason                 string `json:"reason"`
	AcknowledgeUnknownRisk bool   `json:"acknowledge_unknown_risk"`
}

func (service *Service) ForceDeleteNode(ctx context.Context, actor Actor, input ForceNodeDelete) error {
	if !input.AcknowledgeUnknownRisk || !validID(input.NodeID) || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 500 || strings.ContainsAny(input.Reason, "\x00\r\n") {
		return ErrInvalid
	}
	return service.operationMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, true, true); err != nil {
			return nil, err
		}
		var id string
		err := tx.tx.QueryRowContext(tx.ctx, "SELECT id FROM nodes WHERE id=?", input.NodeID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, ErrUnavailable
		}
		if err := tx.forceDetachNode(actor, input); err != nil {
			return nil, err
		}
		if err := tx.one("UPDATE nodes SET agent_token_revoked_at=?,updated_at=? WHERE id=?", tx.now, tx.now, id); err != nil {
			return nil, err
		}
		if err := tx.one("DELETE FROM nodes WHERE id=?", id); err != nil {
			return nil, err
		}
		if err := tx.audit(actor.UserID, "operation.node.force_deleted", id); err != nil {
			return nil, err
		}
		return operationFailure("force-node:"+id, id), nil
	})
}

func (tx transaction) forceDetachNode(actor Actor, input ForceNodeDelete) error {
	if err := tx.exec(`UPDATE operation_attempts SET state=CASE WHEN started_at IS NULL THEN 'canceled' ELSE 'unknown' END,
 retry_actor=?,retry_reason=? WHERE completed_at IS NULL AND target_id IN (SELECT id FROM operation_task_targets WHERE node_id=?)`, actor.UserID, input.Reason, input.NodeID); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE operation_task_targets SET state=CASE WHEN state IN ('queued','leased') THEN 'canceled'
 WHEN state IN ('running','unknown','cancel_requested') THEN 'unknown' ELSE state END,node_id=NULL,updated_at=? WHERE node_id=?`, tx.now.UnixMilli(), input.NodeID); err != nil {
		return err
	}
	return tx.exec(`UPDATE operation_transfer_targets SET state=CASE WHEN state IN ('queued','leased','uploading') THEN 'canceled'
 WHEN state IN ('running','unknown','cancel_requested') THEN 'unknown' ELSE state END,node_id=NULL,updated_at=? WHERE node_id=?`, tx.now.UnixMilli(), input.NodeID)
}
