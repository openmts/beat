package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// PublicVisible 返回节点在指标页展示 GPU 区域的开关；没有记录时默认展示。
func (s *AcceleratorStore) PublicVisible(ctx context.Context, nodeID string) (bool, error) {
	var visible bool
	err := s.db.QueryRowContext(ctx,
		`SELECT visible FROM node_accelerator_visibility WHERE node_id = ?`, nodeID).Scan(&visible)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return visible, err
}

// SetVisibility 保存节点级展示开关并写审计；节点不存在时返回 sql.ErrNoRows。
func (s *AcceleratorStore) SetVisibility(ctx context.Context, actor AcceleratorActor, nodeID string, visible bool) error {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, nodeID).Scan(&exists); err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_accelerator_visibility(node_id, visible, updated_at)
		VALUES(?, ?, ?) ON CONFLICT(node_id) DO UPDATE SET visible = excluded.visible, updated_at = excluded.updated_at`,
		nodeID, visible, now); err != nil {
		return fmt.Errorf("save accelerator visibility: %w", err)
	}
	if err = writeAcceleratorVisibilityAudit(ctx, tx, actor, nodeID, visible, now); err != nil {
		return err
	}
	return tx.Commit()
}

func writeAcceleratorVisibilityAudit(
	ctx context.Context,
	tx *sql.Tx,
	actor AcceleratorActor,
	nodeID string,
	visible bool,
	now time.Time,
) error {
	detail, err := json.Marshal(map[string]bool{"visible": visible})
	if err != nil {
		return err
	}
	event := model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: actor.RequestID, ActorID: actor.UserID,
		ActorUsername: actor.Username, Action: "accelerator.visibility.update",
		ResourceType: "accelerator_visibility", ResourceID: nodeID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail),
		IPAddress: actor.IPAddress, UserAgent: actor.UserAgent,
		SessionPrefix: actor.SessionPrefix, CreatedAt: now,
	}
	return insertAuditEvent(ctx, tx, &event)
}
