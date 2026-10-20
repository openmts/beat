package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

var (
	ErrFleetSummaryNotFound = errors.New("store: fleet summary schedule not found")
	ErrFleetSummaryConflict = errors.New("store: fleet summary schedule changed")
)

const fleetSummaryScheduleColumns = `id, revision, name, cadence, timezone, local_time, weekday,
	name_threshold, enabled, next_run_at, last_period_key, last_run_at, last_result, created_at, updated_at`

// FleetSummaryStore 持久化舰队状态摘要的调度、作用域与运行审计。
type FleetSummaryStore struct {
	db *sql.DB
}

func NewFleetSummaryStore(db *sql.DB) *FleetSummaryStore {
	return &FleetSummaryStore{db: db}
}

func (s *FleetSummaryStore) List(ctx context.Context) ([]model.FleetSummarySchedule, error) {
	return s.list(ctx, "SELECT "+fleetSummaryScheduleColumns+
		" FROM fleet_summary_schedules ORDER BY created_at ASC, name ASC")
}

func (s *FleetSummaryStore) ListDue(
	ctx context.Context,
	now time.Time,
) ([]model.FleetSummarySchedule, error) {
	return s.list(ctx, "SELECT "+fleetSummaryScheduleColumns+
		" FROM fleet_summary_schedules WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at ASC",
		now)
}

func (s *FleetSummaryStore) Get(
	ctx context.Context,
	id string,
) (*model.FleetSummarySchedule, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+fleetSummaryScheduleColumns+" FROM fleet_summary_schedules WHERE id = ?", id)
	schedule, err := scanFleetSummarySchedule(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query fleet summary schedule: %w", err)
	}
	schedules := []model.FleetSummarySchedule{schedule}
	if err := s.loadScopes(ctx, schedules); err != nil {
		return nil, err
	}
	return &schedules[0], nil
}

func (s *FleetSummaryStore) list(
	ctx context.Context,
	query string,
	args ...any,
) ([]model.FleetSummarySchedule, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query fleet summary schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	schedules := []model.FleetSummarySchedule{}
	for rows.Next() {
		schedule, scanErr := scanFleetSummarySchedule(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("scan fleet summary schedule: %w", scanErr)
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fleet summary schedules: %w", err)
	}
	if err := s.loadScopes(ctx, schedules); err != nil {
		return nil, err
	}
	return schedules, nil
}

type fleetSummaryScanner func(...any) error

func scanFleetSummarySchedule(scan fleetSummaryScanner) (model.FleetSummarySchedule, error) {
	schedule := model.FleetSummarySchedule{NodeIDs: []string{}, GroupIDs: []string{}, ChannelIDs: []string{}}
	var weekday, threshold sql.NullInt64
	var nextRun, lastRun sql.NullTime
	var lastResult string
	err := scan(
		&schedule.ID, &schedule.Revision, &schedule.Name, &schedule.Cadence, &schedule.Timezone,
		&schedule.LocalTime, &weekday, &threshold, &schedule.Enabled, &nextRun,
		&schedule.LastPeriodKey, &lastRun, &lastResult, &schedule.CreatedAt, &schedule.UpdatedAt,
	)
	if err != nil {
		return model.FleetSummarySchedule{}, err
	}
	if nextRun.Valid {
		schedule.NextRunAt = &nextRun.Time
	}
	if lastRun.Valid {
		schedule.LastRunAt = &lastRun.Time
	}
	if weekday.Valid {
		value := int(weekday.Int64)
		schedule.Weekday = &value
	}
	if threshold.Valid {
		schedule.NameThreshold = int(threshold.Int64)
	}
	if lastResult != "" {
		schedule.LastResult = &lastResult
	}
	return schedule, nil
}

func (s *FleetSummaryStore) loadScopes(
	ctx context.Context,
	schedules []model.FleetSummarySchedule,
) error {
	if len(schedules) == 0 {
		return nil
	}
	byID := make(map[string]*model.FleetSummarySchedule, len(schedules))
	for index := range schedules {
		schedules[index].NodeIDs = []string{}
		schedules[index].GroupIDs = []string{}
		schedules[index].ChannelIDs = []string{}
		byID[schedules[index].ID] = &schedules[index]
	}
	targets := []struct {
		table  string
		column string
		assign func(*model.FleetSummarySchedule, string)
	}{
		{"fleet_summary_nodes", "node_id", func(schedule *model.FleetSummarySchedule, id string) {
			schedule.NodeIDs = append(schedule.NodeIDs, id)
		}},
		{"fleet_summary_groups", "group_id", func(schedule *model.FleetSummarySchedule, id string) {
			schedule.GroupIDs = append(schedule.GroupIDs, id)
		}},
		{"fleet_summary_channels", "channel_id", func(schedule *model.FleetSummarySchedule, id string) {
			schedule.ChannelIDs = append(schedule.ChannelIDs, id)
		}},
	}
	for _, target := range targets {
		if err := loadFleetSummaryTarget(ctx, s.db, target.table, target.column, byID, target.assign); err != nil {
			return err
		}
	}
	return nil
}

func loadFleetSummaryTarget(
	ctx context.Context,
	db *sql.DB,
	table string,
	column string,
	schedules map[string]*model.FleetSummarySchedule,
	assign func(*model.FleetSummarySchedule, string),
) error {
	rows, err := db.QueryContext(ctx, "SELECT schedule_id, "+column+" FROM "+table+" ORDER BY schedule_id, "+column)
	if err != nil {
		return fmt.Errorf("query fleet summary scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scheduleID, targetID string
		if err := rows.Scan(&scheduleID, &targetID); err != nil {
			return fmt.Errorf("scan fleet summary scope: %w", err)
		}
		if schedule := schedules[scheduleID]; schedule != nil {
			assign(schedule, targetID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate fleet summary scope: %w", err)
	}
	return nil
}
