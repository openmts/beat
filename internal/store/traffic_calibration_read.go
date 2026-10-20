package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const trafficCalibrationColumns = `id,node_id,node_name,kind,effective_at,target_sent,target_received,reason,
	actor_id,actor_name,session_id,request_id,reference_event_id,created_at`

func scanTrafficCalibration(row rowScanner) (model.TrafficCalibrationEvent, error) {
	var event model.TrafficCalibrationEvent
	var nodeID, session, reference sql.NullString
	var effective sql.NullTime
	var sent, received sql.NullInt64
	err := row.Scan(&event.ID, &nodeID, &event.NodeName, &event.Kind, &effective, &sent, &received,
		&event.Reason, &event.Actor.ID, &event.Actor.Name, &session, &event.Actor.RequestID, &reference, &event.CreatedAt)
	if err != nil {
		return model.TrafficCalibrationEvent{}, err
	}
	event.NodeID = optionalString(nodeID)
	event.EffectiveAt = optionalTime(effective, true)
	event.TargetSent = optionalInt64(sent)
	event.TargetReceived = optionalInt64(received)
	event.ReferenceEventID = optionalString(reference)
	event.Actor.SessionID = session.String
	event.CreatedAt = event.CreatedAt.UTC()
	return event, nil
}

func optionalString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	converted := value.String
	return &converted
}

func optionalInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	converted := value.Int64
	return &converted
}

func optionalTime(value sql.NullTime, utc bool) *time.Time {
	if !value.Valid {
		return nil
	}
	converted := value.Time
	if utc {
		converted = converted.UTC()
	}
	return &converted
}

func insertTrafficCalibration(ctx context.Context, tx schemaExecutor, event model.TrafficCalibrationEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO traffic_calibration_events (`+trafficCalibrationColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.NodeID, event.NodeName, event.Kind,
		event.EffectiveAt, event.TargetSent, event.TargetReceived, event.Reason, event.Actor.ID, event.Actor.Name,
		event.Actor.SessionID, event.Actor.RequestID, event.ReferenceEventID, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("append traffic calibration event: %w", err)
	}
	if err := sessionRowsAffected(result, err, "append traffic calibration event"); err != nil {
		return err
	}
	return nil
}

func findTrafficCalibrationByRequest(
	ctx context.Context,
	tx schemaExecutor,
	requestID string,
) (*model.TrafficCalibrationEvent, error) {
	if requestID == "" {
		return nil, nil
	}
	return queryTrafficCalibration(ctx, tx, trafficCalibrationStatement{
		Query: `SELECT ` + trafficCalibrationColumns + ` FROM traffic_calibration_events WHERE request_id = ?`,
		Args:  []any{requestID}})
}

func findTrafficCalibration(
	ctx context.Context,
	tx schemaExecutor,
	nodeID string,
	eventID string,
) (*model.TrafficCalibrationEvent, error) {
	return queryTrafficCalibration(ctx, tx, trafficCalibrationStatement{
		Query: `SELECT ` + trafficCalibrationColumns + ` FROM traffic_calibration_events WHERE node_id = ? AND id = ?`,
		Args:  []any{nodeID, eventID}})
}

func findTrafficCalibrationByID(
	ctx context.Context,
	tx schemaExecutor,
	eventID string,
) (*model.TrafficCalibrationEvent, error) {
	if !model.ValidTrafficCalibrationUUID(eventID) {
		return nil, nil
	}
	return queryTrafficCalibration(ctx, tx, trafficCalibrationStatement{
		Query: `SELECT ` + trafficCalibrationColumns + ` FROM traffic_calibration_events WHERE id = ?`,
		Args:  []any{eventID}})
}

func findTrafficCalibrationRevoke(
	ctx context.Context,
	tx schemaExecutor,
	reference string,
) (*model.TrafficCalibrationEvent, error) {
	return queryTrafficCalibration(ctx, tx, trafficCalibrationStatement{
		Query: `SELECT ` + trafficCalibrationColumns + ` FROM traffic_calibration_events
			WHERE kind = 'revoke' AND reference_event_id = ? ORDER BY rowid DESC LIMIT 1`,
		Args: []any{reference}})
}

// trafficCalibrationStatement 把 SQL 与参数打包，避免读函数出现多参数签名。
type trafficCalibrationStatement struct {
	Query string
	Args  []any
}

func queryTrafficCalibration(
	ctx context.Context,
	tx schemaExecutor,
	statement trafficCalibrationStatement,
) (*model.TrafficCalibrationEvent, error) {
	event, err := scanTrafficCalibration(tx.QueryRowContext(ctx, statement.Query, statement.Args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// latestTrafficCalibration 返回周期内最后一次追加的状态，revoke 也算一次状态。
func latestTrafficCalibration(
	ctx context.Context,
	tx schemaExecutor,
	window TrafficCalibrationWindow,
) (*model.TrafficCalibrationEvent, error) {
	return queryTrafficCalibration(ctx, tx, trafficCalibrationStatement{
		Query: `SELECT ` + trafficCalibrationColumns + ` FROM traffic_calibration_events
			WHERE node_id = ? AND COALESCE(effective_at, created_at) >= ? AND COALESCE(effective_at, created_at) < ?
			ORDER BY rowid DESC LIMIT 1`,
		Args: []any{window.NodeID, window.Start, window.End}})
}

func loadTrafficCalibrationReplay(
	ctx context.Context,
	tx schemaExecutor,
	candidate model.TrafficCalibrationEvent,
) (*model.TrafficCalibrationEvent, bool, error) {
	existing, err := findTrafficCalibrationByRequest(ctx, tx, candidate.Actor.RequestID)
	if err != nil || existing == nil {
		return nil, false, err
	}
	if !sameTrafficCalibrationPayload(*existing, candidate) {
		return nil, false, model.ErrTrafficCalibrationConflict
	}
	return existing, true, nil
}

func sameTrafficCalibrationPayload(existing, candidate model.TrafficCalibrationEvent) bool {
	if existing.Kind != candidate.Kind || existing.Reason != candidate.Reason ||
		existing.Actor.ID != candidate.Actor.ID || existing.Actor.Name != candidate.Actor.Name {
		return false
	}
	if !sameCalibrationNode(existing.NodeID, candidate.NodeID) || !sameCalibrationTarget(existing.TargetSent, candidate.TargetSent) ||
		!sameCalibrationTarget(existing.TargetReceived, candidate.TargetReceived) {
		return false
	}
	return sameCalibrationInstant(existing.EffectiveAt, candidate.EffectiveAt)
}

func sameCalibrationNode(first, second *string) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

func sameCalibrationInstant(first, second *time.Time) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return first.Equal(*second)
}

func sameCalibrationTarget(first, second *int64) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}
