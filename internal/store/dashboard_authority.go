package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type dashboardAuthority struct {
	role                    model.AdminRole
	username, sessionPrefix string
	reauthenticated         sql.NullTime
	actor                   DashboardActor
	now                     time.Time
}

func dashboardActorAuthority(ctx context.Context, transaction *sql.Tx, actor DashboardActor) (dashboardAuthority, error) {
	authority := dashboardAuthority{actor: actor, now: model.NowUTC()}
	err := transaction.QueryRowContext(ctx, `SELECT u.role,u.username,s.token_prefix,s.reauthenticated_until
		FROM admin_sessions s JOIN admin_users u ON u.id=s.user_id
		WHERE s.id=? AND s.user_id=? AND u.enabled=1 AND s.revoked_at IS NULL
		AND s.idle_expires_at>? AND s.absolute_expires_at>?`,
		actor.SessionID, actor.UserID, authority.now, authority.now).Scan(
		&authority.role, &authority.username, &authority.sessionPrefix, &authority.reauthenticated)
	if errors.Is(err, sql.ErrNoRows) {
		return authority, ErrDashboardForbidden
	}
	return authority, err
}

func (authority dashboardAuthority) allow(value model.MetricDashboard) error {
	if value.Scope == "personal" {
		if value.OwnerUserID != authority.actor.UserID || value.NodeID != "" || value.ID == "" {
			return ErrDashboardForbidden
		}
		if id, err := uuid.Parse(value.ID); err != nil || id.String() != value.ID {
			return model.ErrInvalidDashboard
		}
		return nil
	}
	if authority.role != model.AdminRoleOwner || !authority.reauthenticated.Valid ||
		!authority.now.Before(authority.reauthenticated.Time) {
		return ErrDashboardForbidden
	}
	return validateSharedDashboardTarget(value)
}

func validateSharedDashboardTarget(value model.MetricDashboard) error {
	if value.OwnerUserID != "" {
		return ErrDashboardForbidden
	}
	if value.Scope == "node" {
		if value.NodeID == "" || value.ID != "node-"+value.NodeID {
			return ErrDashboardForbidden
		}
		return nil
	}
	expected := map[string]string{"public": model.PublicDashboardID, "admin": model.AdminDashboardID}[value.Scope]
	if expected == "" || value.ID != expected || value.NodeID != "" {
		return ErrDashboardForbidden
	}
	return nil
}

func validateDashboardIdentity(write DashboardWrite, existing *dashboardRecord) error {
	if write.ExpectedRevision < 0 {
		return ErrDashboardConflict
	}
	if existing == nil {
		if write.ExpectedRevision != 0 {
			return ErrDashboardConflict
		}
		return nil
	}
	current := existing.value
	if current.Scope != write.Value.Scope || current.OwnerUserID != write.Value.OwnerUserID || current.NodeID != write.Value.NodeID {
		return ErrDashboardForbidden
	}
	if current.Revision != write.ExpectedRevision {
		return ErrDashboardConflict
	}
	return nil
}

func dashboardActorField(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}
