package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func insertTrafficReportSchedule(
	ctx context.Context,
	tx *sql.Tx,
	schedule *model.TrafficReportSchedule,
) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_report_schedules (
		id, name, cadence, timezone, send_hour, send_minute, weekday, month_day,
		all_nodes, all_channels, enabled, last_period_key, next_run_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
		schedule.ID, schedule.Name, schedule.Cadence, schedule.Timezone,
		schedule.SendHour, schedule.SendMinute, schedule.Weekday, schedule.MonthDay,
		schedule.AllNodes, schedule.AllChannels, schedule.Enabled, schedule.NextRunAt,
		schedule.CreatedAt, schedule.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert traffic report schedule: %w", err)
	}
	return nil
}

func updateTrafficReportSchedule(
	ctx context.Context,
	tx *sql.Tx,
	schedule *model.TrafficReportSchedule,
) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE traffic_report_schedules SET name = ?, cadence = ?,
		timezone = ?, send_hour = ?, send_minute = ?, weekday = ?, month_day = ?, all_nodes = ?,
		all_channels = ?, enabled = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		schedule.Name, schedule.Cadence, schedule.Timezone, schedule.SendHour,
		schedule.SendMinute, schedule.Weekday, schedule.MonthDay, schedule.AllNodes,
		schedule.AllChannels, schedule.Enabled, schedule.NextRunAt, schedule.UpdatedAt, schedule.ID,
	)
	if err != nil {
		return false, fmt.Errorf("update traffic report schedule: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read traffic report update result: %w", err)
	}
	return affected == 1, nil
}

func replaceTrafficReportTargets(
	ctx context.Context,
	tx *sql.Tx,
	schedule *model.TrafficReportSchedule,
) error {
	for _, table := range []string{"traffic_report_schedule_nodes", "traffic_report_schedule_channels"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE schedule_id = ?", schedule.ID); err != nil {
			return fmt.Errorf("clear traffic report targets: %w", err)
		}
	}
	if !schedule.AllNodes {
		if err := insertTrafficReportTargets(ctx, tx, "traffic_report_schedule_nodes", "node_id", schedule.ID, schedule.NodeIDs); err != nil {
			return err
		}
	}
	if !schedule.AllChannels {
		return insertTrafficReportTargets(
			ctx, tx, "traffic_report_schedule_channels", "channel_id", schedule.ID, schedule.ChannelIDs,
		)
	}
	return nil
}

func insertTrafficReportTargets(
	ctx context.Context,
	tx *sql.Tx,
	table string,
	column string,
	scheduleID string,
	ids []string,
) error {
	query := "INSERT INTO " + table + " (schedule_id, " + column + ") VALUES (?, ?)"
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, query, scheduleID, id); err != nil {
			return fmt.Errorf("insert traffic report target: %w", err)
		}
	}
	return nil
}
