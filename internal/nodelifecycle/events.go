package nodelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

type lifecycleEvent struct {
	node                          storedLifecycle
	kind, oldDate, newDate, actor string
	stage, intervals              int
}

func (tx *transaction) event(input lifecycleEvent) (*notificationqueue.Request, error) {
	key := fmt.Sprintf("node_lifecycle:%s:%s:%s:%d", input.node.NodeID, input.node.periodKey, input.kind, input.stage)
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(key)).String()
	result, err := tx.db.ExecContext(tx.ctx, `INSERT INTO node_lifecycle_events(id,node_id,node_name,kind,period_key,stage,old_expires_on,new_expires_on,skipped_intervals,actor,routing_status,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,'pending',?) ON CONFLICT(node_id,period_key,kind,stage) DO NOTHING`, id, input.node.NodeID, input.node.name, input.kind, input.node.periodKey, input.stage, input.oldDate, input.newDate, input.intervals, input.actor, tx.now)
	if err != nil {
		return nil, ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, ErrUnavailable
	}
	if count == 0 {
		return nil, nil
	}
	tx.eventID = id
	message := eventMessage(input)
	return &notificationqueue.Request{SourceKey: key, SourceKind: "node_lifecycle", SourceID: id, EventKind: input.kind, Subject: "Node lifecycle update", Text: message, UseSubscriptions: true,
		Data: map[string]string{"event.kind": input.kind, "event.message": message, "node.id": input.node.NodeID, "node.name": input.node.name}}, nil
}

func eventMessage(input lifecycleEvent) string {
	if input.kind == "node.renewed" {
		if input.actor == "scheduler" {
			return fmt.Sprintf("%s: Date advanced from %s to %s (%d intervals). No payment was performed.", input.node.name, input.oldDate, input.newDate, input.intervals)
		}
		return fmt.Sprintf("%s: Tracked expiry changed from %s to %s.", input.node.name, input.oldDate, input.newDate)
	}
	if input.kind == "node.expired" {
		return fmt.Sprintf("%s: Expiry %s passed %d days ago. Monitoring continues.", input.node.name, input.node.ExpiresOn, input.stage)
	}
	return fmt.Sprintf("%s: Tracked expiry %s is in %d calendar days.", input.node.name, input.node.ExpiresOn, input.stage)
}

type EventQuery struct {
	NodeID, Before string
	Limit          int
}

func (s *Service) Events(ctx context.Context, actor Actor, query EventQuery) ([]model.LifecycleEvent, error) {
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Limit < 1 || query.Limit > 200 || len(query.Before) > 128 {
		return nil, ErrInvalid
	}
	values := []model.LifecycleEvent{}
	err := s.write(ctx, func(tx *transaction) (*notificationqueue.Request, error) {
		if err := tx.authorize(actor, false, false); err != nil {
			return nil, err
		}
		var err error
		values, err = readEvents(ctx, tx.db, query)
		return nil, err
	})
	return values, err
}

func readEvents(ctx context.Context, db *sql.Tx, query EventQuery) (values []model.LifecycleEvent, resultErr error) {
	rows, err := db.QueryContext(ctx, `SELECT id,node_id,kind,period_key,stage,old_expires_on,new_expires_on,skipped_intervals,actor,routing_status,message_id,created_at FROM node_lifecycle_events
 WHERE node_id=? AND (?='' OR (created_at,id)<(SELECT created_at,id FROM node_lifecycle_events WHERE id=?)) ORDER BY created_at DESC,id DESC LIMIT ?`, query.NodeID, query.Before, query.Before, query.Limit)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	values = []model.LifecycleEvent{}
	for rows.Next() {
		var value model.LifecycleEvent
		if err = rows.Scan(&value.ID, &value.NodeID, &value.Kind, &value.PeriodKey, &value.Stage, &value.OldExpiresOn, &value.NewExpiresOn, &value.SkippedIntervals, &value.Actor, &value.RoutingStatus, &value.MessageID, &value.CreatedAt); err != nil {
			return nil, ErrUnavailable
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
