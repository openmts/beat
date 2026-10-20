package store

import (
	"context"

	"github.com/beat/backend/internal/model"
)

// TrafficCalibrationHistoryRequest 是一次有界的历史读取；Cursor 是不透明游标。
type TrafficCalibrationHistoryRequest struct {
	NodeID string
	Limit  int
	Cursor string
}

// TrafficCalibrationHistoryPage 返回新到旧的追加历史与下一页游标。
type TrafficCalibrationHistoryPage struct {
	Events     []model.TrafficCalibrationEvent
	NextCursor string
}

// LatestState 返回周期内最后一次追加的状态（可能是 revoke 或 nil）。
func (store *TrafficCalibrationStore) LatestState(
	ctx context.Context,
	window TrafficCalibrationWindow,
) (*model.TrafficCalibrationEvent, error) {
	if err := validateTrafficCalibrationWindow(window); err != nil {
		return nil, err
	}
	return latestTrafficCalibration(ctx, store.db, window)
}

// Event 读取单个事件；只接受属于该节点的事件。
func (store *TrafficCalibrationStore) Event(
	ctx context.Context,
	nodeID string,
	eventID string,
) (*model.TrafficCalibrationEvent, error) {
	if nodeID == "" || !model.ValidTrafficCalibrationUUID(eventID) {
		return nil, model.ErrTrafficCalibrationInvalid
	}
	return findTrafficCalibration(ctx, store.db, nodeID, eventID)
}

// History 按追加时间倒序分页；未知游标返回无结果而不是猜测顺序。
func (store *TrafficCalibrationStore) History(
	ctx context.Context,
	request TrafficCalibrationHistoryRequest,
) (TrafficCalibrationHistoryPage, error) {
	limit, err := trafficCalibrationPageLimit(request.Limit)
	if err != nil {
		return TrafficCalibrationHistoryPage{}, err
	}
	statement, err := store.historyStatement(ctx, request, limit)
	if err != nil {
		return TrafficCalibrationHistoryPage{}, err
	}
	rows, err := store.db.QueryContext(ctx, statement.Query, statement.Args...)
	if err != nil {
		return TrafficCalibrationHistoryPage{}, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]model.TrafficCalibrationEvent, 0, limit)
	for rows.Next() {
		event, err := scanTrafficCalibration(rows)
		if err != nil {
			return TrafficCalibrationHistoryPage{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return TrafficCalibrationHistoryPage{}, err
	}
	page := TrafficCalibrationHistoryPage{Events: events}
	if len(events) > limit {
		page.Events = events[:limit]
		page.NextCursor = EncodeTrafficCalibrationCursor(page.Events[len(page.Events)-1].ID)
	}
	return page, nil
}

func trafficCalibrationPageLimit(limit int) (int, error) {
	switch {
	case limit <= 0:
		return DefaultTrafficCalibrationPage, nil
	case limit > MaximumTrafficCalibrationRows:
		return 0, model.ErrTrafficCalibrationInvalid
	default:
		return limit, nil
	}
}

func (store *TrafficCalibrationStore) historyStatement(
	ctx context.Context,
	request TrafficCalibrationHistoryRequest,
	limit int,
) (trafficCalibrationStatement, error) {
	if request.NodeID == "" {
		return trafficCalibrationStatement{}, model.ErrTrafficCalibrationInvalid
	}
	statement := trafficCalibrationStatement{
		Query: `SELECT ` + trafficCalibrationColumns + ` FROM traffic_calibration_events WHERE node_id = ?`,
		Args:  []any{request.NodeID}}
	cursor := DecodeTrafficCalibrationCursor(request.Cursor)
	if request.Cursor != "" && cursor == "" {
		return trafficCalibrationStatement{}, model.ErrTrafficCalibrationInvalid
	}
	if cursor != "" {
		anchor, err := findTrafficCalibrationByID(ctx, store.db, cursor)
		if err != nil {
			return trafficCalibrationStatement{}, err
		}
		if anchor == nil {
			return trafficCalibrationStatement{}, model.ErrTrafficCalibrationInvalid
		}
		statement.Query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		statement.Args = append(statement.Args, anchor.CreatedAt, anchor.CreatedAt, anchor.ID)
	}
	statement.Query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	statement.Args = append(statement.Args, limit+1)
	return statement, nil
}
