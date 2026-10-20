package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

type AlertEvaluationLease struct {
	Key       AlertEvaluationKey
	TokenHash []byte
	ExpiresAt time.Time
}

type AlertEvaluationClaim struct {
	Pair     alerteval.DuePair
	WorkerID string
	At       time.Time
}

func (s *AlertEvaluationStore) Due(ctx context.Context, query alerteval.DueQuery) ([]alerteval.DuePair, error) {
	if query.Limit < 1 || query.Limit > alerteval.BatchSize || query.At.IsZero() {
		return nil, alerteval.ErrInvalidPolicy
	}
	statement := `SELECT rule_id,node_id,device_id,next_due_at FROM alert_evaluation_states WHERE next_due_at<=? AND (lease_token_hash IS NULL OR lease_expires_at<=?)`
	args := []any{query.At.UnixMilli(), query.At.UnixMilli()}
	if query.After != nil {
		statement += ` AND (next_due_at,rule_id,node_id,device_id)>(?,?,?,?)`
		args = append(args, query.After.DueAt.UnixMilli(), query.After.RuleID, query.After.NodeID, query.After.DeviceID)
	}
	statement += ` ORDER BY next_due_at,rule_id,node_id,device_id LIMIT ?`
	args = append(args, query.Limit)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	pairs := make([]alerteval.DuePair, 0, query.Limit)
	for rows.Next() {
		var pair alerteval.DuePair
		var due int64
		if err := rows.Scan(&pair.RuleID, &pair.NodeID, &pair.DeviceID, &due); err != nil {
			return nil, err
		}
		pair.DueAt = time.UnixMilli(due).UTC()
		pairs = append(pairs, pair)
	}
	return pairs, rows.Err()
}

// Claim 在实际工作前获取单个租约；不会提前租赁整批而耗尽等待时间。
func (s *AlertEvaluationStore) Claim(ctx context.Context, claim AlertEvaluationClaim) (*AlertEvaluationLease, error) {
	if claim.WorkerID == "" || len(claim.WorkerID) > 36 || claim.At.IsZero() {
		return nil, alerteval.ErrInvalidPolicy
	}
	token := make([]byte, sha256.Size)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(token)
	lease := &AlertEvaluationLease{Key: AlertEvaluationKey{RuleID: claim.Pair.RuleID, NodeID: claim.Pair.NodeID, DeviceID: claim.Pair.DeviceID}, TokenHash: hash[:], ExpiresAt: claim.At.Add(alerteval.LeaseDuration)}
	result, err := s.db.ExecContext(ctx, `UPDATE alert_evaluation_states SET lease_token_hash=?,lease_expires_at=?,worker_id=? WHERE rule_id=? AND node_id=? AND device_id=? AND next_due_at=? AND next_due_at<=? AND (lease_token_hash IS NULL OR lease_expires_at<=?)`, lease.TokenHash, lease.ExpiresAt.UnixMilli(), claim.WorkerID, lease.Key.RuleID, lease.Key.NodeID, lease.Key.DeviceID, claim.Pair.DueAt.UnixMilli(), claim.At.UnixMilli(), claim.At.UnixMilli())
	if err := requireEvaluationMutation(result, err); err != nil {
		return nil, err
	}
	return lease, nil
}

// Release 不推进 next_due_at；失败或取消工作仍显示为待评估。
func (s *AlertEvaluationStore) Release(ctx context.Context, lease AlertEvaluationLease) error {
	if len(lease.TokenHash) != sha256.Size {
		return alerteval.ErrInvalidPolicy
	}
	result, err := s.db.ExecContext(ctx, `UPDATE alert_evaluation_states SET lease_token_hash=NULL,lease_expires_at=NULL,worker_id='' WHERE rule_id=? AND node_id=? AND device_id=? AND lease_token_hash=?`, lease.Key.RuleID, lease.Key.NodeID, lease.Key.DeviceID, lease.TokenHash)
	return requireEvaluationMutation(result, err)
}
