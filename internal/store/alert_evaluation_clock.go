package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

type AlertEvaluationBacklog struct {
	Due          int64
	OldestDueAge time.Duration
}

// RebaseClock 使回退前准备的计划失效，保留事件证据并限制旧租约剩余等待。
func (s *AlertEvaluationStore) RebaseClock(ctx context.Context, at time.Time) (int64, error) {
	if at.IsZero() {
		return 0, alerteval.ErrInvalidPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE alert_evaluation_states SET state_version=state_version+1,
 phase=CASE WHEN active_event_id='' THEN 'normal' ELSE 'firing' END,
 evaluation_status='error',reason_code='clock_rollback',trigger_elapsed_ms=0,recovery_elapsed_ms=0,
 pending_since=NULL,recovery_since=NULL,health_elapsed_ms=0,
 last_evaluated_at=?,last_evidence_observed_at=CASE WHEN last_evidence_observed_at IS NULL THEN NULL ELSE ? END,
 health_last_observed_at=CASE WHEN health_last_observed_at IS NULL THEN NULL ELSE ? END,
 health_status_since=CASE WHEN health_status_since IS NULL THEN NULL ELSE ? END,
 next_due_at=?,updated_at=?,lease_expires_at=CASE WHEN lease_expires_at>? THEN ? ELSE lease_expires_at END
 WHERE last_evaluated_at>?`, at.UnixMilli(), at.UnixMilli(), at.UnixMilli(), at.UnixMilli(), at.UnixMilli(), at.UnixMilli(),
		at.Add(alerteval.LeaseDuration).UnixMilli(), at.Add(alerteval.LeaseDuration).UnixMilli(), at.UnixMilli())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *AlertEvaluationStore) Backlog(ctx context.Context, at time.Time) (AlertEvaluationBacklog, error) {
	if at.IsZero() {
		return AlertEvaluationBacklog{}, alerteval.ErrInvalidPolicy
	}
	var result AlertEvaluationBacklog
	var oldest sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MIN(next_due_at) FROM alert_evaluation_states WHERE next_due_at<=?`, at.UnixMilli()).Scan(&result.Due, &oldest)
	if err != nil {
		return result, err
	}
	if oldest.Valid {
		result.OldestDueAge = max(at.Sub(time.UnixMilli(oldest.Int64)), 0)
	}
	return result, nil
}
