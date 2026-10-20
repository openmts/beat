package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type TrafficReportCompletion struct {
	Schedule  model.TrafficReportSchedule
	PeriodKey string
	NextRun   time.Time
	RunAt     time.Time
}

// CompletePeriodTx 仅在报表与通知队列都可提交时推进周期游标。
func (s *TrafficReportScheduleStore) CompletePeriodTx(ctx context.Context, transaction *sql.Tx, completion TrafficReportCompletion) (bool, error) {
	if transaction == nil {
		return false, errors.New("invalid report period completion")
	}
	if err := validateReportCompletion(completion); err != nil {
		return false, err
	}
	current, err := reportCompletionCurrent(ctx, transaction, completion)
	if err != nil || !current {
		return false, err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE traffic_report_schedules SET last_period_key=?,next_run_at=?,last_run_at=?,
		last_delivery_state='queued',last_delivery_message='queued for delivery',last_delivery_delivered=0,last_delivery_total=0,last_delivery_at=NULL,updated_at=?
		WHERE id=? AND enabled=1 AND updated_at=? AND next_run_at=? AND last_period_key=?`,
		completion.PeriodKey, completion.NextRun, completion.RunAt, completion.RunAt, completion.Schedule.ID,
		completion.Schedule.UpdatedAt, completion.Schedule.NextRunAt, completion.Schedule.LastPeriodKey)
	written, err := eventMutationResult(result, err)
	if err != nil {
		return false, err
	}
	if !written {
		return false, errors.New("report period completion was not persisted")
	}
	if err := recordReportCompletion(ctx, transaction, completion); err != nil {
		return false, err
	}
	return true, nil
}

func validateReportCompletion(completion TrafficReportCompletion) error {
	if completion.PeriodKey == "" || completion.Schedule.ID == "" {
		return errors.New("invalid report period completion")
	}
	if completion.RunAt.IsZero() || !completion.NextRun.After(completion.Schedule.NextRunAt) {
		return errors.New("invalid report completion times")
	}
	return nil
}

func reportCompletionCurrent(ctx context.Context, transaction *sql.Tx, completion TrafficReportCompletion) (bool, error) {
	var current bool
	err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM traffic_report_schedules WHERE id=? AND enabled=1
		AND updated_at=? AND next_run_at=? AND next_run_at<=? AND last_period_key=? AND last_period_key<>?)`,
		completion.Schedule.ID, completion.Schedule.UpdatedAt, completion.Schedule.NextRunAt, completion.RunAt,
		completion.Schedule.LastPeriodKey, completion.PeriodKey).Scan(&current)
	return current, err
}

func recordReportCompletion(ctx context.Context, transaction *sql.Tx, completion TrafficReportCompletion) error {
	return insertAuditEvent(ctx, transaction, &model.AdminAuditEvent{ID: uuid.NewString(), ActorUsername: "system", Action: "traffic_report.queued",
		ResourceType: "traffic_report", ResourceID: completion.Schedule.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: `{}`, CreatedAt: completion.RunAt})
}
