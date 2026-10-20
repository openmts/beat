package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/beat/backend/internal/model"
)

type nodeUpdateMutation struct {
	node   *model.Node
	update NodeUpdate
}

func writeNodeUpdate(ctx context.Context, tx *sql.Tx, input nodeUpdateMutation) error {
	node, update := input.node, input.update
	now := model.NowUTC()
	applyTrafficUpdate(node, update)
	applyNodePresentationUpdate(node, update)
	tagsJSON, err := json.Marshal(node.Tags)
	if err != nil {
		return fmt.Errorf("encoding node tags: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE nodes SET alias = ?, group_id = ?, ssh_public_key = ?, traffic_limit = ?,
		 traffic_limit_type = ?, traffic_reset_day = ?, sort_order = ?, tags = ?, is_public = ?,
		 public_remark = ?, private_remark = ?, updated_at = ? WHERE id = ?`,
		update.Alias, update.GroupID, update.SSHPublicKey, node.TrafficLimit,
		node.TrafficLimitType, node.TrafficResetDay, node.SortOrder, string(tagsJSON), node.IsPublic,
		node.PublicRemark, node.PrivateRemark, now, node.ID,
	)
	if err != nil {
		return fmt.Errorf("updating node: %w", err)
	}

	if err := sessionRowsAffected(result, err, "update node"); err != nil {
		return err
	}
	node.Alias = update.Alias
	node.GroupID = update.GroupID
	node.SSHPublicKey = update.SSHPublicKey
	node.UpdatedAt = now

	return nil
}
