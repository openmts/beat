package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"beat/internal/domain"

	"github.com/google/uuid"
)

var (
	ErrAgentUpgradeInProgress = errors.New("agent upgrade already in progress")
	ErrAgentUpgradeNotFound   = errors.New("agent upgrade batch not found")
)

func (s *Store) CreateAgentUpgradeBatch(ctx context.Context, targetVersion string, items []domain.AgentUpgradeItem, now time.Time) (domain.AgentUpgradeBatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_upgrade_batches WHERE status = 'running'`).Scan(&active); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	if active > 0 {
		return domain.AgentUpgradeBatch{}, ErrAgentUpgradeInProgress
	}

	batchID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_upgrade_batches (id, target_version, status, total_items, created_at, updated_at)
		VALUES (?, ?, 'running', ?, ?, ?)`, batchID, targetVersion, len(items), now.Unix(), now.Unix()); err != nil {
		return domain.AgentUpgradeBatch{}, fmt.Errorf("create agent upgrade batch: %w", err)
	}
	for _, item := range items {
		itemID := item.ID
		if itemID == "" {
			itemID = uuid.NewString()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_upgrade_items (
				id, batch_id, agent_id, node_id, node_name, arch, from_version, to_version,
				artifact_sha256, artifact_size, status, message, attempt, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', '', 0, ?, ?)`,
			itemID, batchID, item.AgentID, item.NodeID, item.NodeName, item.Arch,
			item.FromVersion, targetVersion, item.ArtifactSHA256, item.ArtifactSize,
			now.Unix(), now.Unix()); err != nil {
			return domain.AgentUpgradeBatch{}, fmt.Errorf("create agent upgrade item: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	return s.GetAgentUpgradeBatch(ctx, batchID)
}

func (s *Store) GetAgentUpgradeBatch(ctx context.Context, id string) (domain.AgentUpgradeBatch, error) {
	batch, err := scanAgentUpgradeBatch(s.db.QueryRowContext(ctx, agentUpgradeBatchSelect+` WHERE b.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentUpgradeBatch{}, ErrAgentUpgradeNotFound
	}
	if err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	batch.Items, err = s.listAgentUpgradeItems(ctx, id)
	return batch, err
}

func (s *Store) GetActiveAgentUpgradeBatch(ctx context.Context) (*domain.AgentUpgradeBatch, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM agent_upgrade_batches WHERE status = 'running' ORDER BY created_at DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	batch, err := s.GetAgentUpgradeBatch(ctx, id)
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

func (s *Store) ListAgentUpgradeBatches(ctx context.Context, limit int) ([]domain.AgentUpgradeBatch, error) {
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	rows, err := s.db.QueryContext(ctx, agentUpgradeBatchSelect+` ORDER BY b.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batches := make([]domain.AgentUpgradeBatch, 0)
	for rows.Next() {
		batch, err := scanAgentUpgradeBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s *Store) ListQueuedAgentUpgradeItems(ctx context.Context, batchID, agentID string, limit int) ([]domain.AgentUpgradeItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, agentUpgradeItemSelect+`
		WHERE i.batch_id = ? AND i.status = 'queued' AND (? = '' OR i.agent_id = ?)
		ORDER BY i.created_at ASC LIMIT ?`, batchID, agentID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.AgentUpgradeItem, 0)
	for rows.Next() {
		item, err := scanAgentUpgradeItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkAgentUpgradeDelivered(ctx context.Context, itemID string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_upgrade_items
		SET status = 'delivered', message = '', attempt = attempt + 1,
		    delivered_at = ?, updated_at = ?
		WHERE id = ? AND status = 'queued'`, now.Unix(), now.Unix(), itemID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *Store) UpdateAgentUpgradeProgress(ctx context.Context, agentID, itemID, status, message string, now time.Time) (bool, error) {
	switch status {
	case "installing", "restarting", "failed":
	default:
		return false, fmt.Errorf("invalid agent upgrade progress status %q", status)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_upgrade_items
		SET status = ?, message = ?, updated_at = ?
		WHERE id = ? AND agent_id = ? AND status IN ('delivered', 'installing', 'restarting')`,
		status, truncateText(message, 2048), now.Unix(), itemID, agentID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return affected == 1, err
	}
	if status == "failed" {
		_, _ = s.db.ExecContext(ctx, `UPDATE agent_upgrade_items SET completed_at = ? WHERE id = ?`, now.Unix(), itemID)
	}
	return true, s.refreshAgentUpgradeBatch(ctx, itemID, now)
}

func (s *Store) ReconcileAgentUpgradeVersion(ctx context.Context, agentID, version string, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM agent_upgrade_items
		WHERE agent_id = ? AND to_version = ? AND status IN ('delivered', 'installing', 'restarting')`,
		agentID, version)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return ids, nil
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE agent_upgrade_items
			SET status = 'succeeded', message = 'Agent 已以目标版本重新连接',
			    updated_at = ?, completed_at = ?
			WHERE id = ?`, now.Unix(), now.Unix(), id); err != nil {
			return nil, err
		}
		if err := s.refreshAgentUpgradeBatch(ctx, id, now); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (s *Store) RetryAgentUpgradeBatch(ctx context.Context, batchID string, now time.Time) (domain.AgentUpgradeBatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_upgrade_batches WHERE id = ?`, batchID).Scan(&existing); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	if existing == 0 {
		return domain.AgentUpgradeBatch{}, ErrAgentUpgradeNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_upgrade_items
		SET status = 'queued', message = '等待重试', updated_at = ?,
		    delivered_at = NULL, completed_at = NULL
		WHERE batch_id = ? AND status = 'failed'`, now.Unix(), batchID); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_upgrade_batches SET status = 'running', updated_at = ?, completed_at = NULL
		WHERE id = ?`, now.Unix(), batchID); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	return s.GetAgentUpgradeBatch(ctx, batchID)
}

func (s *Store) FailStaleAgentUpgradeItems(ctx context.Context, olderThan time.Time, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_upgrade_items
		SET status = 'failed', message = 'Agent 在升级确认窗口内未重新连接', updated_at = ?, completed_at = ?
		WHERE status IN ('delivered', 'installing', 'restarting') AND updated_at < ?`,
		now.Unix(), now.Unix(), olderThan.Unix())
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return affected, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE agent_upgrade_batches
		SET status = CASE
			WHEN EXISTS (SELECT 1 FROM agent_upgrade_items i WHERE i.batch_id = agent_upgrade_batches.id AND i.status IN ('queued', 'delivered', 'installing', 'restarting')) THEN 'running'
			ELSE 'completed' END,
			updated_at = ?,
			completed_at = CASE WHEN EXISTS (SELECT 1 FROM agent_upgrade_items i WHERE i.batch_id = agent_upgrade_batches.id AND i.status IN ('queued', 'delivered', 'installing', 'restarting')) THEN NULL ELSE ? END
		WHERE status = 'running'`, now.Unix(), now.Unix()); err != nil {
		return affected, err
	}
	return affected, nil
}

func (s *Store) refreshAgentUpgradeBatch(ctx context.Context, itemID string, now time.Time) error {
	var batchID string
	if err := s.db.QueryRowContext(ctx, `SELECT batch_id FROM agent_upgrade_items WHERE id = ?`, itemID).Scan(&batchID); err != nil {
		return err
	}
	var active int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_upgrade_items
		WHERE batch_id = ? AND status IN ('queued', 'delivered', 'installing', 'restarting')`, batchID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE agent_upgrade_batches SET status = 'running', updated_at = ? WHERE id = ?`, now.Unix(), batchID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_upgrade_batches
		SET status = CASE WHEN EXISTS (SELECT 1 FROM agent_upgrade_items WHERE batch_id = ? AND status = 'failed') THEN 'partial' ELSE 'completed' END,
		    updated_at = ?, completed_at = ?
		WHERE id = ?`, batchID, now.Unix(), now.Unix(), batchID)
	return err
}

func (s *Store) listAgentUpgradeItems(ctx context.Context, batchID string) ([]domain.AgentUpgradeItem, error) {
	rows, err := s.db.QueryContext(ctx, agentUpgradeItemSelect+` WHERE i.batch_id = ? ORDER BY i.node_name COLLATE NOCASE ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.AgentUpgradeItem, 0)
	for rows.Next() {
		item, err := scanAgentUpgradeItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const agentUpgradeBatchSelect = `
	SELECT b.id, b.target_version, b.status,
	       (SELECT COUNT(*) FROM agent_upgrade_items i WHERE i.batch_id = b.id),
	       (SELECT COUNT(*) FROM agent_upgrade_items i WHERE i.batch_id = b.id AND i.status = 'queued'),
	       (SELECT COUNT(*) FROM agent_upgrade_items i WHERE i.batch_id = b.id AND i.status IN ('delivered', 'installing', 'restarting')),
	       (SELECT COUNT(*) FROM agent_upgrade_items i WHERE i.batch_id = b.id AND i.status = 'succeeded'),
	       (SELECT COUNT(*) FROM agent_upgrade_items i WHERE i.batch_id = b.id AND i.status = 'failed'),
	       b.created_at, b.updated_at, b.completed_at
	FROM agent_upgrade_batches b`

const agentUpgradeItemSelect = `
	SELECT i.id, i.batch_id, i.agent_id, i.node_id, i.node_name, i.arch,
	       i.from_version, i.to_version, i.artifact_sha256, i.artifact_size,
	       i.status, i.message, i.attempt, i.created_at, i.updated_at,
	       i.delivered_at, i.completed_at
	FROM agent_upgrade_items i`

type upgradeScanner interface {
	Scan(dest ...any) error
}

func scanAgentUpgradeBatch(row upgradeScanner) (domain.AgentUpgradeBatch, error) {
	var batch domain.AgentUpgradeBatch
	var createdAt, updatedAt int64
	var completedAt sql.NullInt64
	if err := row.Scan(&batch.ID, &batch.TargetVersion, &batch.Status, &batch.Total,
		&batch.Queued, &batch.Delivered, &batch.Succeeded, &batch.Failed,
		&createdAt, &updatedAt, &completedAt); err != nil {
		return domain.AgentUpgradeBatch{}, err
	}
	batch.CreatedAt = time.Unix(createdAt, 0).UTC()
	batch.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if completedAt.Valid {
		value := time.Unix(completedAt.Int64, 0).UTC()
		batch.CompletedAt = &value
	}
	return batch, nil
}

func scanAgentUpgradeItem(row upgradeScanner) (domain.AgentUpgradeItem, error) {
	var item domain.AgentUpgradeItem
	var createdAt, updatedAt int64
	var deliveredAt, completedAt sql.NullInt64
	if err := row.Scan(&item.ID, &item.BatchID, &item.AgentID, &item.NodeID, &item.NodeName,
		&item.Arch, &item.FromVersion, &item.ToVersion, &item.ArtifactSHA256, &item.ArtifactSize,
		&item.Status, &item.Message, &item.Attempt, &createdAt, &updatedAt,
		&deliveredAt, &completedAt); err != nil {
		return domain.AgentUpgradeItem{}, err
	}
	item.CreatedAt = time.Unix(createdAt, 0).UTC()
	item.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if deliveredAt.Valid {
		value := time.Unix(deliveredAt.Int64, 0).UTC()
		item.DeliveredAt = &value
	}
	if completedAt.Valid {
		value := time.Unix(completedAt.Int64, 0).UTC()
		item.CompletedAt = &value
	}
	return item, nil
}
