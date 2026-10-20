package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrSecurityFilterInvalid 表示查询过滤值不在允许的固定词表内。
var ErrSecurityFilterInvalid = errors.New("store: security event filter is invalid")

// ErrSecurityEventNotFound 表示安全事件不存在。
var ErrSecurityEventNotFound = errors.New("store: security event not found")

const (
	securityEventDefaultLimit = 50
	securityMaximumDeliveries = 50
	securityEventMaximumLimit = 200
	securityDeliveryPending   = "pending"
	securityDeliveryFailed    = "failed"
	securityDeliveryDelivered = "delivered"
	securityDeliveryNone      = "none"
)

const securityEventColumns = `e.id, e.kind, e.severity, COALESCE(e.actor_user_id, ''), e.actor_display_name,
	e.auth_method, e.source_class, e.source_family, e.source_prefix, e.browser_family, e.os_family,
	e.device_class, e.country_code, e.region, e.reasons_json, e.policy_revision, e.routing_state,
	e.audit_event_id, COALESCE(e.message_id, ''), e.occurred_at, e.retain_until, e.acknowledged_at,
	COALESCE(e.acknowledged_by, ''), e.acknowledged_note, COALESCE(d.channels, 0), COALESCE(d.failed, 0),
	COALESCE(d.pending, 0), COALESCE(d.delivered, 0)`

const securityEventDeliveryJoin = ` LEFT JOIN (
	SELECT message_id, COUNT(*) AS channels,
		SUM(state IN ('dead', 'cancelled')) AS failed,
		SUM(state IN ('pending', 'leased')) AS pending,
		SUM(state = 'succeeded') AS delivered
	FROM notification_deliveries GROUP BY message_id) d ON d.message_id = e.message_id`

// securityDeliveryValues 是允许的投递过滤词表；与上面的子句一一对应。
var securityDeliveryValues = []string{
	securityDeliveryPending, securityDeliveryDelivered, securityDeliveryFailed, securityDeliveryNone,
}

var securityDeliveryClauses = map[string]string{
	securityDeliveryPending:   "COALESCE(d.pending, 0) > 0",
	securityDeliveryFailed:    "COALESCE(d.failed, 0) > 0",
	securityDeliveryDelivered: "COALESCE(d.delivered, 0) > 0",
	securityDeliveryNone:      "COALESCE(d.channels, 0) = 0",
}

func (store *AdminStore) ListSecurityEvents(
	ctx context.Context, filter model.SecurityEventFilter,
) (model.SecurityEventPage, error) {
	filter = normalizeSecurityEventFilter(filter)
	where, args, err := securityEventWhere(filter)
	if err != nil {
		return model.SecurityEventPage{}, err
	}
	var total int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM security_events e"+
		securityEventDeliveryJoin+where, args...).Scan(&total); err != nil {
		return model.SecurityEventPage{}, fmt.Errorf("count security events: %w", err)
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := store.db.QueryContext(ctx, "SELECT "+securityEventColumns+" FROM security_events e"+
		securityEventDeliveryJoin+where+" ORDER BY e.occurred_at DESC, e.id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return model.SecurityEventPage{}, fmt.Errorf("list security events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := []model.SecurityEvent{}
	for rows.Next() {
		event, err := scanSecurityEvent(rows)
		if err != nil {
			return model.SecurityEventPage{}, fmt.Errorf("scan security event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return model.SecurityEventPage{}, fmt.Errorf("iterate security events: %w", err)
	}
	return model.SecurityEventPage{Events: events, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (store *AdminStore) GetSecurityEvent(ctx context.Context, id string) (model.SecurityEvent, error) {
	row := store.db.QueryRowContext(ctx, "SELECT "+securityEventColumns+" FROM security_events e"+
		securityEventDeliveryJoin+" WHERE e.id = ?", id)
	event, err := scanSecurityEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.SecurityEvent{}, ErrSecurityEventNotFound
	}
	if err != nil {
		return model.SecurityEvent{}, fmt.Errorf("read security event: %w", err)
	}
	deliveries, err := securityEventDeliveries(ctx, store.db, event.MessageID)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	event.Deliveries = deliveries
	return event, nil
}

// securityEventDeliveries 只读取脱敏投递元数据：渠道名、状态、尝试次数与稳定错误码。
func securityEventDeliveries(ctx context.Context, query rowQuerier, messageID string) ([]model.SecurityEventDelivery, error) {
	if messageID == "" {
		return nil, nil
	}
	rows, err := query.QueryContext(ctx, `SELECT d.id, d.channel_id, COALESCE(c.name, ''), d.state,
		d.attempt_count, d.error_code, d.updated_at
		FROM notification_deliveries d LEFT JOIN alert_channels c ON c.id = d.channel_id
		WHERE d.message_id = ? ORDER BY d.created_at LIMIT ?`, messageID, securityMaximumDeliveries)
	if err != nil {
		return nil, fmt.Errorf("list security event deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	deliveries := []model.SecurityEventDelivery{}
	for rows.Next() {
		var delivery model.SecurityEventDelivery
		var updatedAt int64
		if err := rows.Scan(&delivery.DeliveryID, &delivery.ChannelID, &delivery.ChannelName, &delivery.State,
			&delivery.AttemptCount, &delivery.ErrorCode, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan security event delivery: %w", err)
		}
		delivery.UpdatedAt = time.UnixMilli(updatedAt).UTC()
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate security event deliveries: %w", err)
	}
	return deliveries, nil
}

func scanSecurityEvent(row rowScanner) (model.SecurityEvent, error) {
	var (
		event     model.SecurityEvent
		encoded   string
		occurred  int64
		retain    int64
		ackedAt   sql.NullInt64
		delivered int
	)
	err := row.Scan(&event.ID, &event.Kind, &event.Severity, &event.ActorUserID, &event.ActorDisplayName,
		&event.AuthMethod, &event.SourceClass, &event.SourceFamily, &event.SourcePrefix, &event.BrowserFamily,
		&event.OSFamily, &event.DeviceClass, &event.CountryCode, &event.Region, &encoded, &event.PolicyRevision,
		&event.RoutingState, &event.AuditEventID, &event.MessageID, &occurred, &retain, &ackedAt,
		&event.AcknowledgedBy, &event.AcknowledgedNote, &event.DeliveryChannels, &event.DeliveryFailures,
		&event.DeliveryPending, &delivered)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &event.Reasons); err != nil {
		return model.SecurityEvent{}, err
	}
	event.OccurredAt = time.UnixMilli(occurred).UTC()
	event.RetainUntil = time.UnixMilli(retain).UTC()
	event.AcknowledgedAt = nullableTimeFromMillis(ackedAt)
	event.DeliveryState = deliveryStateOf(event, delivered)
	return event, nil
}

func deliveryStateOf(event model.SecurityEvent, delivered int) string {
	switch {
	case event.DeliveryFailures > 0:
		return securityDeliveryFailed
	case event.DeliveryPending > 0:
		return securityDeliveryPending
	case delivered > 0:
		return securityDeliveryDelivered
	default:
		return securityDeliveryNone
	}
}

func nullableTimeFromMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	moment := time.UnixMilli(value.Int64).UTC()
	return &moment
}

func normalizeSecurityEventFilter(filter model.SecurityEventFilter) model.SecurityEventFilter {
	if filter.Limit <= 0 {
		filter.Limit = securityEventDefaultLimit
	}
	if filter.Limit > securityEventMaximumLimit {
		filter.Limit = securityEventMaximumLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func securityEventWhere(filter model.SecurityEventFilter) (string, []any, error) {
	if err := validateSecurityEventFilter(filter); err != nil {
		return "", nil, err
	}
	clauses := []string{}
	args := []any{}
	add := func(column, value string) {
		if value != "" {
			clauses = append(clauses, column+" = ?")
			args = append(args, value)
		}
	}
	add("e.kind", filter.Kind)
	add("e.severity", filter.Severity)
	add("e.actor_user_id", filter.ActorID)
	add("e.source_class", filter.SourceClass)
	add("e.routing_state", filter.RoutingState)
	if filter.Reason != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(e.reasons_json) WHERE value = ?)")
		args = append(args, filter.Reason)
	}
	if filter.DeliveryState != "" {
		clauses = append(clauses, securityDeliveryClauses[filter.DeliveryState])
	}
	if len(clauses) == 0 {
		return "", nil, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

// validateSecurityEventFilter 拒绝词表外的过滤值，避免静默忽略客户端输入。
func validateSecurityEventFilter(filter model.SecurityEventFilter) error {
	choices := []struct {
		value   string
		allowed []string
	}{
		{filter.Kind, securityEventKindValues},
		{filter.Severity, securitySeverityValues},
		{filter.SourceClass, securitySourceClassValues},
		{filter.RoutingState, securityRoutingValues},
		{filter.Reason, model.SecurityReasonCodes()},
		{filter.DeliveryState, securityDeliveryValues},
	}
	for _, choice := range choices {
		if choice.value == "" || slices.Contains(choice.allowed, choice.value) {
			continue
		}
		return ErrSecurityFilterInvalid
	}
	return nil
}
