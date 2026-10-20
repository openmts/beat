package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	trafficCalibrationSchemaVersion = 21
	maximumTrafficCalibrationRows   = 1_000_000
)

// validateTrafficCalibrationLedger 校验归档快照里的流量校准账本。
//
// 返回 handled=false 表示这份备份来自更早的能力版本，恢复方按“空账本 + 原始结算”
// 处理即可。返回 handled=true 时任何不一致都必须让恢复失败，因为账本决定对外可见的
// 计费口径：宁可拒绝恢复，也不能让损坏的账本静默变成零用量。
func validateTrafficCalibrationLedger(
	ctx context.Context,
	sqlitePath string,
) (handled bool, resultErr error) {
	database, err := openArchivedSQLite(sqlitePath)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	version, err := archivedSchemaVersion(ctx, database)
	if err != nil {
		return false, err
	}
	if version < trafficCalibrationSchemaVersion {
		return false, nil
	}
	events, err := archivedTrafficCalibrationEvents(ctx, database)
	if err != nil {
		return true, err
	}
	nodes, err := archivedNodeIDs(ctx, database)
	if err != nil {
		return true, err
	}
	if err := validateCalibrationLedger(events, nodes); err != nil {
		return true, err
	}
	return true, nil
}

func archivedSchemaVersion(ctx context.Context, database *sql.DB) (int64, error) {
	var version sql.NullInt64
	err := database.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version)
	if err != nil {
		if isNoSuchTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read archived schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return version.Int64, nil
}

// archivedTrafficCalibrationEvents 按 rowid 读回追加顺序；账本只追加，因此 rowid
// 就是真实的追加顺序，也是“当时正在生效”的判定依据。
func archivedTrafficCalibrationEvents(
	ctx context.Context,
	database *sql.DB,
) ([]model.TrafficCalibrationEvent, error) {
	rows, err := database.QueryContext(ctx, `SELECT id,node_id,node_name,kind,effective_at,target_sent,
		target_received,reason,actor_id,actor_name,session_id,request_id,reference_event_id,created_at
		FROM traffic_calibration_events ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("read archived traffic calibrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]model.TrafficCalibrationEvent, 0)
	for rows.Next() {
		if len(events) >= maximumTrafficCalibrationRows {
			return nil, errors.New("archived traffic calibration ledger exceeds the row limit")
		}
		event, err := scanArchivedCalibration(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read archived traffic calibrations: %w", err)
	}
	return events, nil
}

type calibrationScanner interface{ Scan(dest ...any) error }

func scanArchivedCalibration(row calibrationScanner) (model.TrafficCalibrationEvent, error) {
	var event model.TrafficCalibrationEvent
	var nodeID, session, reference sql.NullString
	var effective sql.NullTime
	var sent, received sql.NullInt64
	err := row.Scan(&event.ID, &nodeID, &event.NodeName, &event.Kind, &effective, &sent, &received,
		&event.Reason, &event.Actor.ID, &event.Actor.Name, &session, &event.Actor.RequestID,
		&reference, &event.CreatedAt)
	if err != nil {
		return model.TrafficCalibrationEvent{}, fmt.Errorf("scan archived traffic calibration: %w", err)
	}
	event.NodeID = archivedOptionalString(nodeID)
	event.EffectiveAt = archivedOptionalTime(effective)
	event.TargetSent = archivedOptionalInt64(sent)
	event.TargetReceived = archivedOptionalInt64(received)
	event.ReferenceEventID = archivedOptionalString(reference)
	event.Actor.SessionID = session.String
	event.CreatedAt = event.CreatedAt.UTC()
	return event, nil
}

func archivedOptionalString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	converted := value.String
	return &converted
}

func archivedOptionalInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	converted := value.Int64
	return &converted
}

func archivedOptionalTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	converted := value.Time.UTC()
	return &converted
}

func archivedNodeIDs(ctx context.Context, database *sql.DB) (map[string]bool, error) {
	rows, err := database.QueryContext(ctx, "SELECT id FROM nodes")
	if err != nil {
		return nil, fmt.Errorf("read archived node identifiers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	identifiers := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan archived node identifier: %w", err)
		}
		identifiers[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read archived node identifiers: %w", err)
	}
	return identifiers, nil
}

// validateCalibrationLedger 重放全部事件，校验形状、生效秒精度、节点关联与引用链。
func validateCalibrationLedger(events []model.TrafficCalibrationEvent, nodes map[string]bool) error {
	active := map[string]string{}
	appended := map[string]model.TrafficCalibrationEvent{}
	for _, event := range events {
		if err := validateArchivedCalibrationEvent(event, nodes); err != nil {
			return err
		}
		key := calibrationLedgerKey(event)
		if event.Kind == model.TrafficCalibrationSet {
			appended[event.ID] = event
			active[key] = event.ID
			continue
		}
		if err := validateArchivedRevoke(event, active[key], appended); err != nil {
			return err
		}
		delete(active, key)
	}
	return nil
}

func calibrationLedgerKey(event model.TrafficCalibrationEvent) string {
	if event.NodeID != nil {
		return *event.NodeID
	}
	// 节点删除后 node_id 置空；此时只能用名称快照分组，名称在节点表里唯一。
	return "name:" + event.NodeName
}

func validateArchivedCalibrationEvent(event model.TrafficCalibrationEvent, nodes map[string]bool) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("archived traffic calibration %s is invalid", event.ID)
	}
	if event.EffectiveAt != nil {
		effective := event.EffectiveAt.UTC()
		if !effective.Equal(effective.Truncate(time.Second)) {
			return fmt.Errorf("archived traffic calibration %s is not representable in MTS seconds", event.ID)
		}
	}
	if event.NodeID != nil && !nodes[*event.NodeID] {
		return fmt.Errorf("archived traffic calibration %s references a missing node", event.ID)
	}
	return nil
}

func validateArchivedRevoke(
	event model.TrafficCalibrationEvent,
	activeID string,
	appended map[string]model.TrafficCalibrationEvent,
) error {
	reference := *event.ReferenceEventID
	if activeID != reference {
		return fmt.Errorf("archived traffic calibration %s does not revoke the active set", event.ID)
	}
	target, found := appended[reference]
	if !found || !target.CreatedAt.Before(event.CreatedAt) && !target.CreatedAt.Equal(event.CreatedAt) {
		return fmt.Errorf("archived traffic calibration %s references an unknown or later event", event.ID)
	}
	if !sameArchivedNode(target.NodeID, event.NodeID) {
		return fmt.Errorf("archived traffic calibration %s revokes another node's event", event.ID)
	}
	return nil
}

func sameArchivedNode(first, second *string) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}
