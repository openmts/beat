package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type AlertRuleStore struct {
	db *sql.DB
}

func NewAlertRuleStore(db *sql.DB) *AlertRuleStore {
	return &AlertRuleStore{db: db}
}

func (s *AlertRuleStore) ListAlertRules(ctx context.Context) ([]model.AlertRule, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, description, metric, operator, threshold, duration, severity, enabled, created_at, updated_at FROM alert_rules ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("querying alert rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	rules := []model.AlertRule{}
	for rows.Next() {
		var r model.AlertRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Metric, &r.Operator, &r.Threshold, &r.Duration, &r.Severity, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning alert rule: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating alert rules: %w", err)
	}

	return rules, nil
}

func (s *AlertRuleStore) CreateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error) {
	now := model.NowUTC()
	rule.ID = uuid.New().String()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO alert_rules (id, name, description, metric, operator, threshold, duration, severity, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		rule.ID, rule.Name, rule.Description, rule.Metric, rule.Operator, rule.Threshold, rule.Duration, rule.Severity, rule.Enabled, rule.CreatedAt, rule.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting alert rule: %w", err)
	}

	return rule, nil
}

func (s *AlertRuleStore) UpdateAlertRule(ctx context.Context, id string, rule *model.AlertRule) (*model.AlertRule, error) {
	now := model.NowUTC()
	rule.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		"UPDATE alert_rules SET name = ?, description = ?, metric = ?, operator = ?, threshold = ?, duration = ?, severity = ?, enabled = ?, updated_at = ? WHERE id = ?",
		rule.Name, rule.Description, rule.Metric, rule.Operator, rule.Threshold, rule.Duration, rule.Severity, rule.Enabled, rule.UpdatedAt, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating alert rule: %w", err)
	}

	rule.ID = id

	return rule, nil
}

func (s *AlertRuleStore) DeleteAlertRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM alert_rules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting alert rule: %w", err)
	}

	return nil
}

func (s *AlertRuleStore) ListEnabledRules(ctx context.Context) ([]model.AlertRule, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, description, metric, operator, threshold, duration, severity, enabled, created_at, updated_at FROM alert_rules WHERE enabled = 1 ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("querying enabled alert rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	rules := []model.AlertRule{}
	for rows.Next() {
		var r model.AlertRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Metric, &r.Operator, &r.Threshold, &r.Duration, &r.Severity, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning alert rule: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating alert rules: %w", err)
	}

	return rules, nil
}

type AlertEventStore struct {
	db *sql.DB
}

func NewAlertEventStore(db *sql.DB) *AlertEventStore {
	return &AlertEventStore{db: db}
}

func (s *AlertEventStore) ListAlertEvents(ctx context.Context) ([]model.AlertEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, rule_id, node_id, message, value, status, triggered_at, resolved_at FROM alert_events ORDER BY triggered_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("querying alert events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events := []model.AlertEvent{}
	for rows.Next() {
		var e model.AlertEvent
		var resolvedAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.RuleID, &e.NodeID, &e.Message, &e.Value, &e.Status, &e.TriggeredAt, &resolvedAt); err != nil {
			return nil, fmt.Errorf("scanning alert event: %w", err)
		}
		if resolvedAt.Valid {
			e.ResolvedAt = &resolvedAt.Time
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating alert events: %w", err)
	}

	return events, nil
}

func (s *AlertEventStore) CreateEvent(ctx context.Context, event *model.AlertEvent) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO alert_events (id, rule_id, node_id, message, value, status, triggered_at, resolved_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		event.ID, event.RuleID, event.NodeID, event.Message, event.Value, event.Status, event.TriggeredAt, event.ResolvedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting alert event: %w", err)
	}
	return nil
}

func (s *AlertEventStore) GetActiveEvent(ctx context.Context, ruleID, nodeID string) (*model.AlertEvent, error) {
	var e model.AlertEvent
	var resolvedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		"SELECT id, rule_id, node_id, message, value, status, triggered_at, resolved_at FROM alert_events WHERE rule_id = ? AND node_id = ? AND status = ? LIMIT 1",
		ruleID, nodeID, model.AlertStatusTriggered,
	).Scan(&e.ID, &e.RuleID, &e.NodeID, &e.Message, &e.Value, &e.Status, &e.TriggeredAt, &resolvedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying active event: %w", err)
	}
	if resolvedAt.Valid {
		e.ResolvedAt = &resolvedAt.Time
	}
	return &e, nil
}

func (s *AlertEventStore) UpdateEvent(ctx context.Context, event *model.AlertEvent) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE alert_events SET status = ?, resolved_at = ? WHERE id = ?",
		event.Status, event.ResolvedAt, event.ID,
	)
	if err != nil {
		return fmt.Errorf("updating alert event: %w", err)
	}
	return nil
}
