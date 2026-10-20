package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type TrafficReportTest struct {
	ScheduleID string
	Actor      EventMutationActor
	At         time.Time
}

func (store *TrafficReportScheduleStore) RecordTestRunTx(ctx context.Context, transaction *sql.Tx, test TrafficReportTest) error {
	if transaction == nil || test.ScheduleID == "" || test.At.IsZero() {
		return errors.New("invalid traffic report test")
	}
	if err := validateEventActor(ctx, transaction, test.Actor); err != nil {
		return err
	}
	var id string
	if err := transaction.QueryRowContext(ctx, "SELECT id FROM traffic_report_schedules WHERE id=?", test.ScheduleID).Scan(&id); err != nil {
		return err
	}
	event := model.AdminAuditEvent{ID: uuid.NewString(), ActorID: test.Actor.UserID, Action: "traffic_report.test_queued", ResourceType: "traffic_report", ResourceID: id,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: `{}`, RequestID: test.Actor.RequestID, IPAddress: test.Actor.IPAddress, UserAgent: test.Actor.UserAgent, CreatedAt: test.At}
	if err := transaction.QueryRowContext(ctx, `SELECT u.username,s.token_prefix FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id WHERE u.id=? AND s.id=?`, test.Actor.UserID, test.Actor.SessionID).Scan(&event.ActorUsername, &event.SessionPrefix); err != nil {
		return err
	}
	return insertAuditEvent(ctx, transaction, &event)
}
