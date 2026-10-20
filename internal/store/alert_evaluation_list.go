package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

const EvaluationDefaultPage = 50

const EvaluationMaximumPage = 200

const evaluationMaximumCursorBytes = 512

type AlertEvaluationQuery struct {
	RuleID, NodeID, DeviceID, Phase, Status, Kind, Cursor string
	Limit                                                 int
	DeviceScoped                                          bool
}

type AlertEvaluationPage struct {
	Items      []model.AlertEvaluationState `json:"items"`
	NextCursor string                       `json:"next_cursor,omitempty"`
}

type AlertEvaluationEventPage struct {
	Items      []model.AlertEvaluationEvent `json:"items"`
	NextCursor string                       `json:"next_cursor,omitempty"`
}

type evaluationCursor struct {
	DeviceID string `json:"device,omitempty"`
	RuleID   string `json:"rule,omitempty"`
	NodeID   string `json:"node,omitempty"`
	At       int64  `json:"at,omitempty"`
	ID       string `json:"id,omitempty"`
}

func decodeEvaluationCursor(encoded string) (evaluationCursor, error) {
	var cursor evaluationCursor
	if encoded == "" {
		return cursor, nil
	}
	if len(encoded) > evaluationMaximumCursorBytes {
		return cursor, alerteval.ErrInvalidPolicy
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, alerteval.ErrInvalidPolicy
	}
	if err := json.Unmarshal(data, &cursor); err != nil || len(cursor.RuleID) > 128 || len(cursor.NodeID) > 128 || len(cursor.ID) > 128 {
		return cursor, alerteval.ErrInvalidPolicy
	}
	return cursor, nil
}

func encodeEvaluationCursor(cursor evaluationCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func evaluationQueryLimit(limit int) (int, error) {
	if limit == 0 {
		return EvaluationDefaultPage, nil
	}
	if limit < 1 || limit > EvaluationMaximumPage {
		return 0, alerteval.ErrInvalidPolicy
	}
	return limit, nil
}

func (s *AlertEvaluationStore) List(ctx context.Context, query AlertEvaluationQuery) (AlertEvaluationPage, error) {
	page := AlertEvaluationPage{Items: []model.AlertEvaluationState{}}
	statement, args, limit, err := evaluationStateQuery(query)
	if err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		state, err := scanEvaluationState(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, *state)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeEvaluationCursor(evaluationCursor{RuleID: last.RuleID, NodeID: last.NodeID, DeviceID: last.DeviceID})
	}
	return page, err
}

func evaluationStateQuery(query AlertEvaluationQuery) (string, []any, int, error) {
	limit, err := evaluationQueryLimit(query.Limit)
	if err != nil {
		return "", nil, 0, err
	}
	cursor, err := decodeEvaluationCursor(query.Cursor)
	if err != nil {
		return "", nil, 0, err
	}
	filters, args := []string{"1=1"}, []any{}
	if query.Phase != "" && !evaluationChoice(query.Phase, "normal", "pending", "firing", "recovering") {
		return "", nil, 0, alerteval.ErrInvalidPolicy
	}
	if query.Status != "" && !evaluationChoice(query.Status, "ok", "no_data", "error", "disabled", "out_of_scope") {
		return "", nil, 0, alerteval.ErrInvalidPolicy
	}
	for _, filter := range []struct{ column, value string }{{"rule_id", query.RuleID}, {"node_id", query.NodeID}, {"phase", query.Phase}, {"evaluation_status", query.Status}} {
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
		if cursor.RuleID == "" || cursor.NodeID == "" {
			return "", nil, 0, alerteval.ErrInvalidPolicy
		}
		filters = append(filters, "(rule_id,node_id,device_id)>(?,?,?)")
		args = append(args, cursor.RuleID, cursor.NodeID, cursor.DeviceID)
	}
	statement := "SELECT " + evaluationStateReadColumns + " FROM alert_evaluation_states WHERE " + strings.Join(filters, " AND ") + " ORDER BY rule_id,node_id,device_id LIMIT ?"
	return statement, append(args, limit+1), limit, nil
}

func evaluationChoice(value string, allowed ...string) bool {
	for _, choice := range allowed {
		if value == choice {
			return true
		}
	}
	return false
}
