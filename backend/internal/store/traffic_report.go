package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const trafficReportScheduleColumns = `id, name, cadence, timezone, send_hour, send_minute,
	weekday, month_day, all_nodes, all_channels, enabled, last_period_key, last_run_at,
	next_run_at, last_delivery_state, last_delivery_message, last_delivery_delivered,
	last_delivery_total, last_delivery_at, created_at, updated_at`

type TrafficReportScheduleStore struct {
	db *sql.DB
}

func NewTrafficReportScheduleStore(db *sql.DB) *TrafficReportScheduleStore {
	return &TrafficReportScheduleStore{db: db}
}

func (s *TrafficReportScheduleStore) List(
	ctx context.Context,
) ([]model.TrafficReportSchedule, error) {
	query := "SELECT " + trafficReportScheduleColumns +
		" FROM traffic_report_schedules ORDER BY created_at ASC"
	return s.list(ctx, query)
}

func (s *TrafficReportScheduleStore) ListDue(
	ctx context.Context,
	now time.Time,
) ([]model.TrafficReportSchedule, error) {
	query := "SELECT " + trafficReportScheduleColumns +
		" FROM traffic_report_schedules WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at ASC"
	return s.list(ctx, query, now)
}

func (s *TrafficReportScheduleStore) Get(
	ctx context.Context,
	id string,
) (*model.TrafficReportSchedule, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+trafficReportScheduleColumns+" FROM traffic_report_schedules WHERE id = ?",
		id,
	)
	schedule, err := scanTrafficReportSchedule(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query traffic report schedule: %w", err)
	}
	schedules := []model.TrafficReportSchedule{schedule}
	if err := s.loadTargets(ctx, schedules); err != nil {
		return nil, err
	}
	return &schedules[0], nil
}

func (s *TrafficReportScheduleStore) Create(
	ctx context.Context,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	now := model.NowUTC()
	schedule.ID = uuid.NewString()
	schedule.CreatedAt = now
	schedule.UpdatedAt = now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin traffic report create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertTrafficReportSchedule(ctx, tx, schedule); err != nil {
		return nil, err
	}
	if err := replaceTrafficReportTargets(ctx, tx, schedule); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit traffic report create: %w", err)
	}
	return schedule, nil
}

func (s *TrafficReportScheduleStore) Update(
	ctx context.Context,
	id string,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin traffic report update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	schedule.ID = id
	schedule.UpdatedAt = model.NowUTC()
	found, err := updateTrafficReportSchedule(ctx, tx, schedule)
	if err != nil || !found {
		return nil, err
	}
	if err := replaceTrafficReportTargets(ctx, tx, schedule); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit traffic report update: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *TrafficReportScheduleStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin traffic report delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"traffic_report_schedule_nodes", "traffic_report_schedule_channels"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE schedule_id = ?", id); err != nil {
			return fmt.Errorf("delete traffic report targets: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_report_schedules WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete traffic report schedule: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit traffic report delete: %w", err)
	}
	return nil
}

func (s *TrafficReportScheduleStore) ClaimDue(
	ctx context.Context,
	id string,
	periodKey string,
	nextRun time.Time,
	dueAt time.Time,
) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE traffic_report_schedules
		SET last_period_key = ?, next_run_at = ?, updated_at = ?
		WHERE id = ? AND enabled = 1 AND next_run_at <= ? AND last_period_key <> ?`,
		periodKey, nextRun, model.NowUTC(), id, dueAt, periodKey,
	)
	if err != nil {
		return false, fmt.Errorf("claim traffic report schedule: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read traffic report claim result: %w", err)
	}
	return affected == 1, nil
}

func (s *TrafficReportScheduleStore) CompleteRun(
	ctx context.Context,
	id string,
	runAt time.Time,
	status model.TrafficReportDeliveryStatus,
) error {
	_, err := s.db.ExecContext(ctx, `UPDATE traffic_report_schedules SET last_run_at = ?,
		last_delivery_state = ?, last_delivery_message = ?, last_delivery_delivered = ?,
		last_delivery_total = ?, last_delivery_at = ?, updated_at = ? WHERE id = ?`,
		runAt, status.State, status.Message, status.Delivered, status.Total,
		status.DeliveredAt, model.NowUTC(), id,
	)
	if err != nil {
		return fmt.Errorf("complete traffic report run: %w", err)
	}
	return nil
}

func (s *TrafficReportScheduleStore) list(
	ctx context.Context,
	query string,
	args ...any,
) ([]model.TrafficReportSchedule, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query traffic report schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	schedules := []model.TrafficReportSchedule{}
	for rows.Next() {
		schedule, scanErr := scanTrafficReportSchedule(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("scan traffic report schedule: %w", scanErr)
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate traffic report schedules: %w", err)
	}
	if err := s.loadTargets(ctx, schedules); err != nil {
		return nil, err
	}
	return schedules, nil
}

type trafficReportScanner func(...any) error

func scanTrafficReportSchedule(scan trafficReportScanner) (model.TrafficReportSchedule, error) {
	var schedule model.TrafficReportSchedule
	var lastRun, lastDelivery sql.NullTime
	var state, message string
	var delivered, total int
	err := scan(
		&schedule.ID, &schedule.Name, &schedule.Cadence, &schedule.Timezone,
		&schedule.SendHour, &schedule.SendMinute, &schedule.Weekday, &schedule.MonthDay,
		&schedule.AllNodes, &schedule.AllChannels, &schedule.Enabled, &schedule.LastPeriodKey,
		&lastRun, &schedule.NextRunAt, &state, &message, &delivered, &total,
		&lastDelivery, &schedule.CreatedAt, &schedule.UpdatedAt,
	)
	if lastRun.Valid {
		schedule.LastRunAt = &lastRun.Time
	}
	if state != "" && lastDelivery.Valid {
		schedule.LastDelivery = &model.TrafficReportDeliveryStatus{
			State: state, Message: message, Delivered: delivered, Total: total,
			DeliveredAt: lastDelivery.Time,
		}
	}
	return schedule, err
}
