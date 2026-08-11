package store

import (
	"context"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

func (s *NodeStore) MarkStaleNodesOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE nodes SET status = ?, updated_at = ?
		WHERE status = ? AND last_seen IS NOT NULL AND last_seen < ?`,
		model.NodeStatusOffline, model.NowUTC(), model.NodeStatusOnline, cutoff)
	if err != nil {
		return 0, fmt.Errorf("marking stale nodes offline: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reading stale node update count: %w", err)
	}
	return updated, nil
}
