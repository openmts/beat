package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

const evaluationHistoryColumns = `id,rule_id,node_id,rule_revision,epoch_id,kind,reason_code,occurred_at,notified_at,closed_at,source_key,device_id`

func (s *AlertEvaluationStore) ListEvents(ctx context.Context, query AlertEvaluationQuery) (AlertEvaluationEventPage, error) {
	page := AlertEvaluationEventPage{Items: []model.AlertEvaluationEvent{}}
	statement, args, limit, err := evaluationHistoryQuery(query)
	if err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		event, err := scanEvaluationHistory(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, event)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeEvaluationCursor(evaluationCursor{At: last.OccurredAt.UnixMilli(), ID: last.ID})
	}
	return page, err
}

func scanEvaluationHistory(row metricDefinitionScanner) (model.AlertEvaluationEvent, error) {
	var event model.AlertEvaluationEvent
	var at int64
	var notified, closed sql.NullInt64
	err := row.Scan(&event.ID, &event.RuleID, &event.NodeID, &event.RuleRevision, &event.EpochID, &event.Kind, &event.Reason, &at, &notified, &closed, &event.SourceKey, &event.DeviceID, &event.DeviceName)
	event.OccurredAt = time.UnixMilli(at).UTC()
	event.NotifiedAt, event.ClosedAt = nullableMillis(notified), nullableMillis(closed)
	return event, err
}

func evaluationHistoryQuery(query AlertEvaluationQuery) (string, []any, int, error) {
	limit, err := evaluationQueryLimit(query.Limit)
	if err != nil {
		return "", nil, 0, err
	}
	cursor, err := decodeEvaluationCursor(query.Cursor)
	if err != nil {
		return "", nil, 0, err
	}
	if query.Kind != "" && !evaluationChoice(query.Kind, "no_data", "error", "recovered") {
		return "", nil, 0, alerteval.ErrInvalidPolicy
	}
	filters, args := []string{"1=1"}, []any{}
	for _, filter := range []struct{ column, value string }{{"rule_id", query.RuleID}, {"node_id", query.NodeID}, {"kind", query.Kind}} {
		if filter.value != "" {
			filters = append(filters, filter.column+"=?")
			args = append(args, filter.value)
		}
	}
	if query.DeviceScoped || query.DeviceID != "" {
		if query.DeviceID != "" && !model.ValidAcceleratorDeviceID(query.DeviceID) {
			return "", nil, 0, alerteval.ErrInvalidPolicy
		}
		filters = append(filters, "device_id=?")
		args = append(args, query.DeviceID)
	}
	if query.Cursor != "" {
		if cursor.ID == "" {
			return "", nil, 0, alerteval.ErrInvalidPolicy
		}
		filters = append(filters, "(occurred_at<? OR (occurred_at=? AND id>?))")
		args = append(args, cursor.At, cursor.At, cursor.ID)
	}
	statement := "SELECT " + evaluationHistoryReadColumns + " FROM alert_evaluation_events WHERE " + strings.Join(filters, " AND ") + " ORDER BY occurred_at DESC,id LIMIT ?"
	return statement, append(args, limit+1), limit, nil
}

const evaluationHistoryReadColumns = evaluationHistoryColumns + `,COALESCE((SELECT 'GPU '||ordinal||' · '||model FROM node_accelerators a WHERE a.node_id=alert_evaluation_events.node_id AND a.device_id=alert_evaluation_events.device_id),'')`
