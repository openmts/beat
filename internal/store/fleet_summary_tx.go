package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const (
	// FleetSummaryStaleRunAge 之后仍未投递的队列运行按“投递结果未知”结算。
	FleetSummaryStaleRunAge = 7 * 24 * time.Hour
	// FleetSummaryRunRetention 限定恢复后保留的运行审计窗口。
	FleetSummaryRunRetention = 180 * 24 * time.Hour
)

// FleetSummaryCompletion 描述一次自动调度的认领：写入运行审计并原子推进游标。
type FleetSummaryCompletion struct {
	Schedule  model.FleetSummarySchedule
	PeriodKey string
	NextRun   time.Time
	RunAt     time.Time
	Run       model.FleetSummaryRun
}

// FleetSummaryTest 描述一次手动测试发送，永不推进调度游标。
type FleetSummaryTest struct {
	ScheduleID string
	Actor      EventMutationActor
	At         time.Time
	Run        model.FleetSummaryRun
}

// CompleteRunTx 仅在运行审计与通知 outbox 都可提交时推进周期游标。
func (s *FleetSummaryStore) CompleteRunTx(
	ctx context.Context,
	transaction *sql.Tx,
	completion FleetSummaryCompletion,
) (bool, error) {
	if transaction == nil {
		return false, errors.New("invalid fleet summary completion")
	}
	if err := validateFleetSummaryCompletion(completion); err != nil {
		return false, err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE fleet_summary_schedules
		SET last_period_key = ?, next_run_at = ?, last_run_at = ?, last_result = ?, updated_at = ?
		WHERE id = ? AND enabled = 1 AND updated_at = ? AND next_run_at = ?
			AND next_run_at <= ? AND last_period_key <> ?`,
		completion.PeriodKey, completion.NextRun, completion.RunAt, model.FleetSummaryResultQueued,
		completion.RunAt, completion.Schedule.ID, completion.Schedule.UpdatedAt,
		completion.Schedule.NextRunAt, completion.RunAt, completion.PeriodKey)
	written, err := eventMutationResult(result, err)
	if err != nil {
		return false, fmt.Errorf("claim fleet summary period: %w", err)
	}
	if !written {
		return false, nil
	}
	run := completion.Run
	run.ScheduleID = completion.Schedule.ID
	run.PeriodKey = completion.PeriodKey
	run.Cutoff = *completion.Schedule.NextRunAt
	run.Status = model.FleetSummaryResultQueued
	if run.CreatedAt.IsZero() {
		run.CreatedAt = completion.RunAt
	}
	if err := insertFleetSummaryRun(ctx, transaction, run); err != nil {
		return false, err
	}
	return true, recordFleetSummaryQueued(ctx, transaction, completion)
}

// RecordTestRunTx 记录手动测试运行与审计事件，不改动调度游标。
func (s *FleetSummaryStore) RecordTestRunTx(
	ctx context.Context,
	transaction *sql.Tx,
	test FleetSummaryTest,
) error {
	if transaction == nil || test.ScheduleID == "" || test.At.IsZero() {
		return errors.New("invalid fleet summary test run")
	}
	if err := validateEventActor(ctx, transaction, test.Actor); err != nil {
		return err
	}
	var id string
	if err := transaction.QueryRowContext(ctx,
		"SELECT id FROM fleet_summary_schedules WHERE id = ?", test.ScheduleID).Scan(&id); err != nil {
		return fmt.Errorf("query fleet summary schedule: %w", err)
	}
	run := test.Run
	run.ScheduleID = id
	run.Kind = model.FleetSummaryRunTest
	run.CreatedAt = test.At
	if run.Cutoff.IsZero() {
		run.Cutoff = test.At
	}
	if err := insertFleetSummaryRun(ctx, transaction, run); err != nil {
		return err
	}
	event := model.AdminAuditEvent{ID: uuid.NewString(), ActorID: test.Actor.UserID, Action: "fleet_summary.test_queued",
		ResourceType: "fleet_summary", ResourceID: id, Outcome: model.AuditOutcomeSuccess, DetailJSON: `{}`,
		RequestID: test.Actor.RequestID, IPAddress: test.Actor.IPAddress, UserAgent: test.Actor.UserAgent, CreatedAt: test.At}
	if err := transaction.QueryRowContext(ctx, `SELECT u.username, s.token_prefix FROM admin_users u
		JOIN admin_sessions s ON s.user_id = u.id WHERE u.id = ? AND s.id = ?`,
		test.Actor.UserID, test.Actor.SessionID).Scan(&event.ActorUsername, &event.SessionPrefix); err != nil {
		return fmt.Errorf("query fleet summary test actor: %w", err)
	}
	return insertAuditEvent(ctx, transaction, &event)
}

// ReconcileFleetSummaryRestore 收敛恢复后的运行审计：把投递结果未知的陈旧运行标记为失败，
// 并裁剪超出保留窗口的历史。调度游标本身保持不变，由服务层的单次补跑语义收敛。
func ReconcileFleetSummaryRestore(ctx context.Context, path string, now time.Time) (resultErr error) {
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_txlock=immediate&_pragma=foreign_keys(1)"}
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var present int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE name = 'fleet_summary_runs'").Scan(&present); err != nil {
		return fmt.Errorf("inspect restored fleet summary runs: %w", err)
	}
	if present == 0 {
		return nil
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE fleet_summary_runs SET status = 'failed',
		result_code = 'delivery_unknown', completed_at = ? WHERE status = 'queued' AND created_at < ?`,
		now, now.Add(-FleetSummaryStaleRunAge)); err != nil {
		return fmt.Errorf("settle restored fleet summary runs: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM fleet_summary_runs WHERE created_at < ?", now.Add(-FleetSummaryRunRetention)); err != nil {
		return fmt.Errorf("trim restored fleet summary runs: %w", err)
	}
	return tx.Commit()
}

func validateFleetSummaryCompletion(completion FleetSummaryCompletion) error {
	if completion.PeriodKey == "" || completion.Schedule.ID == "" {
		return errors.New("invalid fleet summary completion")
	}
	if completion.Schedule.NextRunAt == nil {
		return errors.New("invalid fleet summary completion times")
	}
	if completion.RunAt.IsZero() || !completion.NextRun.After(*completion.Schedule.NextRunAt) {
		return errors.New("invalid fleet summary completion times")
	}
	return nil
}

func recordFleetSummaryQueued(
	ctx context.Context,
	transaction *sql.Tx,
	completion FleetSummaryCompletion,
) error {
	return insertAuditEvent(ctx, transaction, &model.AdminAuditEvent{ID: uuid.NewString(),
		ActorUsername: "system", Action: "fleet_summary.queued", ResourceType: "fleet_summary",
		ResourceID: completion.Schedule.ID, Outcome: model.AuditOutcomeSuccess,
		DetailJSON: `{}`, CreatedAt: completion.RunAt})
}

func insertFleetSummaryRun(ctx context.Context, transaction *sql.Tx, run model.FleetSummaryRun) error {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	onlineNames, err := json.Marshal(run.Counts.OnlineNames)
	if err != nil {
		return fmt.Errorf("encode fleet summary names: %w", err)
	}
	offlineNames, err := json.Marshal(run.Counts.OfflineNames)
	if err != nil {
		return fmt.Errorf("encode fleet summary names: %w", err)
	}
	if run.Status == "" {
		run.Status = model.FleetSummaryResultQueued
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = model.NowUTC()
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO fleet_summary_runs
		(id, schedule_id, schedule_name, cadence, timezone, period_key, cutoff, kind, status,
		 online_count, offline_count, total_count, online_names, offline_names, channel_count,
		 result_code, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.ScheduleID, run.ScheduleName, run.Cadence, run.Timezone, run.PeriodKey, run.Cutoff,
		run.Kind, run.Status, run.Counts.Online, run.Counts.Offline, run.Counts.Total,
		string(onlineNames), string(offlineNames), run.ChannelCount, run.ResultCode,
		run.CreatedAt, run.CompletedAt); err != nil {
		return fmt.Errorf("insert fleet summary run: %w", err)
	}
	return nil
}
