package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

var ErrMetricCatalogForbidden = errors.New("current owner session and recent authentication are required")

func authorizeMetricPolicy(ctx context.Context, db schemaExecutor, change *MetricCatalogPolicyChange) error {
	if change.SessionID == "" || change.Audit.ActorID == "" {
		return ErrMetricCatalogForbidden
	}
	now := model.NowUTC()
	var role model.AdminRole
	var until sql.NullTime
	err := db.QueryRowContext(ctx, `SELECT u.role,u.username,s.token_prefix,s.reauthenticated_until
		FROM admin_sessions s JOIN admin_users u ON u.id=s.user_id
		WHERE s.id=? AND s.user_id=? AND u.enabled=1 AND s.revoked_at IS NULL
		AND s.idle_expires_at>? AND s.absolute_expires_at>?`, change.SessionID, change.Audit.ActorID, now, now).
		Scan(&role, &change.Audit.ActorUsername, &change.Audit.SessionPrefix, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMetricCatalogForbidden
	}
	if err != nil {
		return err
	}
	if role != model.AdminRoleOwner || !until.Valid || !now.Before(until.Time) {
		return ErrMetricCatalogForbidden
	}
	return nil
}
