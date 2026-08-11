package store

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func (s *TrafficReportScheduleStore) loadTargets(
	ctx context.Context,
	schedules []model.TrafficReportSchedule,
) error {
	if len(schedules) == 0 {
		return nil
	}
	byID := make(map[string]*model.TrafficReportSchedule, len(schedules))
	for index := range schedules {
		schedules[index].NodeIDs = []string{}
		schedules[index].ChannelIDs = []string{}
		byID[schedules[index].ID] = &schedules[index]
	}
	if err := s.loadTargetTable(
		ctx, "traffic_report_schedule_nodes", "node_id", byID,
		func(schedule *model.TrafficReportSchedule, id string) {
			schedule.NodeIDs = append(schedule.NodeIDs, id)
		},
	); err != nil {
		return err
	}
	return s.loadTargetTable(
		ctx, "traffic_report_schedule_channels", "channel_id", byID,
		func(schedule *model.TrafficReportSchedule, id string) {
			schedule.ChannelIDs = append(schedule.ChannelIDs, id)
		},
	)
}

func (s *TrafficReportScheduleStore) loadTargetTable(
	ctx context.Context,
	table string,
	column string,
	schedules map[string]*model.TrafficReportSchedule,
	appendTarget func(*model.TrafficReportSchedule, string),
) error {
	query := "SELECT schedule_id, " + column + " FROM " + table + " ORDER BY schedule_id, " + column
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query traffic report targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scheduleID, targetID string
		if err := rows.Scan(&scheduleID, &targetID); err != nil {
			return fmt.Errorf("scan traffic report target: %w", err)
		}
		if schedule := schedules[scheduleID]; schedule != nil {
			appendTarget(schedule, targetID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate traffic report targets: %w", err)
	}
	return nil
}
