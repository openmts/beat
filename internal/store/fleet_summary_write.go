package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var fleetSummaryScopeTables = []struct {
	table  string
	column string
	ids    func(*model.FleetSummarySchedule) []string
}{
	{"fleet_summary_nodes", "node_id", func(schedule *model.FleetSummarySchedule) []string { return schedule.NodeIDs }},
	{"fleet_summary_groups", "group_id", func(schedule *model.FleetSummarySchedule) []string { return schedule.GroupIDs }},
	{"fleet_summary_channels", "channel_id", func(schedule *model.FleetSummarySchedule) []string { return schedule.ChannelIDs }},
}

func (s *FleetSummaryStore) Create(
	ctx context.Context,
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	now := model.NowUTC()
	schedule.ID = uuid.NewString()
	schedule.Revision = 1
	schedule.CreatedAt = now
	schedule.UpdatedAt = now
	if schedule.NextRunAt == nil {
		return nil, errors.New("fleet summary schedule requires a next run time")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin fleet summary create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO fleet_summary_schedules
		(id, revision, name, cadence, timezone, local_time, weekday, name_threshold, enabled,
		 next_run_at, last_period_key, last_run_at, last_result, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', NULL, '', ?, ?)`,
		schedule.ID, schedule.Revision, schedule.Name, schedule.Cadence, schedule.Timezone,
		schedule.LocalTime, nullableWeekday(schedule.Weekday), schedule.NameThreshold, schedule.Enabled,
		schedule.NextRunAt, schedule.CreatedAt, schedule.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert fleet summary schedule: %w", err)
	}
	if err := replaceFleetSummaryScopes(ctx, tx, schedule); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit fleet summary create: %w", err)
	}
	return schedule, nil
}

func (s *FleetSummaryStore) Update(
	ctx context.Context,
	id string,
	expectedRevision int,
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	if schedule.NextRunAt == nil {
		return nil, errors.New("fleet summary schedule requires a next run time")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin fleet summary update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE fleet_summary_schedules SET revision = revision + 1,
		name = ?, cadence = ?, timezone = ?, local_time = ?, weekday = ?, name_threshold = ?,
		enabled = ?, next_run_at = ?, updated_at = ?
		WHERE id = ? AND revision = ?`,
		schedule.Name, schedule.Cadence, schedule.Timezone, schedule.LocalTime,
		nullableWeekday(schedule.Weekday), schedule.NameThreshold, schedule.Enabled,
		schedule.NextRunAt, schedule.UpdatedAt, id, expectedRevision)
	written, err := eventMutationResult(result, err)
	if err != nil {
		return nil, fmt.Errorf("update fleet summary schedule: %w", err)
	}
	if !written {
		return nil, s.writeConflict(ctx, id)
	}
	if err := replaceFleetSummaryScopes(ctx, tx, schedule); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit fleet summary update: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *FleetSummaryStore) Delete(ctx context.Context, id string, expectedRevision int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin fleet summary delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx,
		"DELETE FROM fleet_summary_schedules WHERE id = ? AND revision = ?", id, expectedRevision)
	removed, err := eventMutationResult(result, err)
	if err != nil {
		return fmt.Errorf("delete fleet summary schedule: %w", err)
	}
	if !removed {
		return s.writeConflict(ctx, id)
	}
	for _, scope := range fleetSummaryScopeTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+scope.table+" WHERE schedule_id = ?", id); err != nil {
			return fmt.Errorf("delete fleet summary scope: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fleet summary delete: %w", err)
	}
	return nil
}

// writeConflict 区分“计划不存在”与“版本已被并发修改”，让 API 返回 404 或 409。
func (s *FleetSummaryStore) writeConflict(ctx context.Context, id string) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM fleet_summary_schedules WHERE id = ?)", id).Scan(&exists); err != nil {
		return fmt.Errorf("query fleet summary schedule: %w", err)
	}
	if !exists {
		return ErrFleetSummaryNotFound
	}
	return ErrFleetSummaryConflict
}

func nullableWeekday(weekday *int) any {
	if weekday == nil {
		return nil
	}
	return *weekday
}

func replaceFleetSummaryScopes(
	ctx context.Context,
	tx *sql.Tx,
	schedule *model.FleetSummarySchedule,
) error {
	for _, scope := range fleetSummaryScopeTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+scope.table+" WHERE schedule_id = ?", schedule.ID); err != nil {
			return fmt.Errorf("clear fleet summary scope: %w", err)
		}
		query := "INSERT INTO " + scope.table + " (schedule_id, " + scope.column + ") VALUES (?, ?)"
		for _, id := range scope.ids(schedule) {
			if _, err := tx.ExecContext(ctx, query, schedule.ID, id); err != nil {
				return fmt.Errorf("insert fleet summary scope: %w", err)
			}
		}
	}
	return nil
}
