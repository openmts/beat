package nodelifecycle

import (
	"context"
	"database/sql"
	"time"
)

type Snapshot struct {
	Scheduled            int64      `json:"scheduled"`
	DueToday             int64      `json:"due_today"`
	Expired              int64      `json:"expired"`
	Untracked            int64      `json:"untracked"`
	CalendarAdvancements int64      `json:"calendar_advancements"`
	LastSuccessAt        *time.Time `json:"last_success_at,omitempty"`
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	var value Snapshot
	settings, err := scanSettings(s.db.QueryRowContext(ctx, settingsQuery))
	if err != nil {
		return value, err
	}
	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return value, ErrInvalid
	}
	today := s.now().In(zone).Format(time.DateOnly)
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.expires_on>?),0),COALESCE(SUM(l.expires_on=?),0),
 COALESCE(SUM(l.expires_on!='' AND l.expires_on<?),0),COALESCE(SUM(l.expires_on IS NULL OR l.expires_on=''),0)
 FROM nodes n LEFT JOIN node_lifecycle l ON l.id=n.id`, today, today, today).Scan(&value.Scheduled, &value.DueToday, &value.Expired, &value.Untracked)
	if err != nil {
		return value, ErrUnavailable
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_lifecycle_events WHERE kind='node.renewed' AND actor='scheduler'`).Scan(&value.CalendarAdvancements); err != nil {
		return value, ErrUnavailable
	}
	var last sql.NullTime
	if err = s.db.QueryRowContext(ctx, `SELECT last_success_at FROM lifecycle_settings WHERE id=1`).Scan(&last); err != nil {
		return value, ErrUnavailable
	}
	if last.Valid {
		value.LastSuccessAt = &last.Time
	}
	return value, nil
}

// Prune 保留当前周期与仍有outbox证据的事件，每次最多清理1000条旧历史。
func (s *Service) Prune(ctx context.Context) (resultErr error) {
	defer func() { s.observe(resultErr) }()
	var days int
	if err := s.db.QueryRowContext(ctx, `SELECT dead_retention_days FROM notification_settings WHERE id=1`).Scan(&days); err != nil {
		return ErrUnavailable
	}
	if days < 1 || days > 3650 {
		return ErrUnavailable
	}
	cutoff := s.now().AddDate(0, 0, -days)
	_, err := s.db.ExecContext(ctx, `DELETE FROM node_lifecycle_events WHERE id IN(
 SELECT e.id FROM node_lifecycle_events e WHERE e.created_at<?
 AND NOT EXISTS(SELECT 1 FROM node_lifecycle l WHERE l.id=e.node_id AND l.period_key=e.period_key)
 AND NOT EXISTS(SELECT 1 FROM notification_messages m WHERE m.id=e.message_id)
 ORDER BY e.created_at LIMIT 1000)`, cutoff)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
