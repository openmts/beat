package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrInvalidAlertEventQuery 表示分页游标、过滤器或 limit 不合法；调用方应返回 400。
var ErrInvalidAlertEventQuery = errors.New("invalid alert event query")

const (
	// AlertEventDefaultPage 与 AlertEventMaximumPage 约束历史告警事件的分页上限。
	AlertEventDefaultPage = 50
	AlertEventMaximumPage = 200

	alertEventMaximumCursorBytes = 512
	alertEventCursorVersion      = 1
)

// AlertEventQuery 是服务端筛选与游标分页的输入；时间过滤按 UTC 归一化。
type AlertEventQuery struct {
	RuleID, NodeID, Status, Cursor string
	From, To                       *time.Time
	Limit                          int
}

// AlertEventPage 只返回一页事件与下一页游标，绝不整表下发。
type AlertEventPage struct {
	Items      []model.AlertEvent `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// alertEventCursor 绑定归一化过滤器，跨过滤复用或版本不符一律拒绝。
type alertEventCursor struct {
	RuleID  string `json:"rule,omitempty"`
	NodeID  string `json:"node,omitempty"`
	Status  string `json:"status,omitempty"`
	At      int64  `json:"at"`
	ID      string `json:"id"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	HasFrom bool   `json:"has_from,omitempty"`
	HasTo   bool   `json:"has_to,omitempty"`
	Version int    `json:"v"`
}

func (s *AlertEventStore) ListAlertEventsPage(ctx context.Context, query AlertEventQuery) (AlertEventPage, error) {
	page := AlertEventPage{Items: []model.AlertEvent{}}
	statement, args, limit, err := alertEventPageQuery(query)
	if err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		event, scanErr := scanAlertEvent(rows)
		if scanErr != nil {
			return page, scanErr
		}
		page.Items = append(page.Items, *event)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeAlertEventCursor(query, last)
	}
	return page, err
}

func alertEventPageQuery(query AlertEventQuery) (string, []any, int, error) {
	limit, err := alertEventQueryLimit(query.Limit)
	if err != nil {
		return "", nil, 0, err
	}
	if query.Status != "" && !validAlertEventStatus(query.Status) {
		return "", nil, 0, ErrInvalidAlertEventQuery
	}
	cursor, err := decodeAlertEventCursor(query.Cursor)
	if err != nil {
		return "", nil, 0, err
	}
	filters, args := []string{"1=1"}, []any{}
	for _, filter := range []struct{ column, value string }{
		{"rule_id", query.RuleID}, {"node_id", query.NodeID}, {"status", query.Status},
	} {
		if filter.value != "" {
			filters = append(filters, filter.column+"=?")
			args = append(args, filter.value)
		}
	}
	if query.From != nil {
		filters = append(filters, "triggered_at>=?")
		args = append(args, query.From.UTC())
	}
	if query.To != nil {
		filters = append(filters, "triggered_at<=?")
		args = append(args, query.To.UTC())
	}
	if query.Cursor != "" {
		if !alertEventCursorMatches(cursor, query) {
			return "", nil, 0, ErrInvalidAlertEventQuery
		}
		filters = append(filters, "(triggered_at<? OR (triggered_at=? AND id<?))")
		at := time.UnixMilli(cursor.At).UTC()
		args = append(args, at, at, cursor.ID)
	}
	statement := "SELECT " + alertEventColumns + " FROM alert_events WHERE " +
		strings.Join(filters, " AND ") + " ORDER BY triggered_at DESC,id DESC LIMIT ?"
	return statement, append(args, limit+1), limit, nil
}

func validAlertEventStatus(status string) bool {
	switch model.AlertStatus(status) {
	case model.AlertStatusTriggered, model.AlertStatusAcknowledged, model.AlertStatusResolved:
		return true
	default:
		return false
	}
}

func alertEventQueryLimit(limit int) (int, error) {
	if limit == 0 {
		return AlertEventDefaultPage, nil
	}
	if limit < 1 || limit > AlertEventMaximumPage {
		return 0, ErrInvalidAlertEventQuery
	}
	return limit, nil
}

func encodeAlertEventCursor(query AlertEventQuery, last model.AlertEvent) (string, error) {
	cursor := alertEventCursor{
		RuleID: query.RuleID, NodeID: query.NodeID, Status: query.Status,
		At: last.TriggeredAt.UnixMilli(), ID: last.ID, Version: alertEventCursorVersion,
	}
	if query.From != nil {
		cursor.From, cursor.HasFrom = query.From.UTC().Format(time.RFC3339Nano), true
	}
	if query.To != nil {
		cursor.To, cursor.HasTo = query.To.UTC().Format(time.RFC3339Nano), true
	}
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeAlertEventCursor(encoded string) (alertEventCursor, error) {
	var cursor alertEventCursor
	if encoded == "" {
		return cursor, nil
	}
	if len(encoded) > alertEventMaximumCursorBytes {
		return cursor, ErrInvalidAlertEventQuery
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, ErrInvalidAlertEventQuery
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return cursor, ErrInvalidAlertEventQuery
	}
	if cursor.Version != alertEventCursorVersion || cursor.ID == "" || len(cursor.ID) > 128 {
		return cursor, ErrInvalidAlertEventQuery
	}
	return cursor, nil
}

// alertEventCursorMatches 校验游标与当前请求的归一化过滤器一致。
func alertEventCursorMatches(cursor alertEventCursor, query AlertEventQuery) bool {
	if cursor.RuleID != query.RuleID || cursor.NodeID != query.NodeID || cursor.Status != query.Status {
		return false
	}
	if cursor.HasFrom != (query.From != nil) || cursor.HasTo != (query.To != nil) {
		return false
	}
	if query.From != nil && cursor.From != query.From.UTC().Format(time.RFC3339Nano) {
		return false
	}
	if query.To != nil && cursor.To != query.To.UTC().Format(time.RFC3339Nano) {
		return false
	}
	return true
}
