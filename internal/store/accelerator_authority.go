package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrAcceleratorForbidden = errors.New("accelerator policy permission denied")

type acceleratorAuthorization struct {
	actor *AcceleratorActor
	scope AcceleratorPolicyScope
	write bool
}

func acceleratorAuthorize(ctx context.Context, db schemaExecutor, input acceleratorAuthorization) error {
	if err := validateAcceleratorAuthority(input); err != nil {
		return err
	}
	now := time.Now().UTC()
	var role string
	var until sql.NullTime
	err := db.QueryRowContext(ctx, `SELECT u.role,s.reauthenticated_until,u.username,s.token_prefix FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id
 WHERE u.id=? AND s.id=? AND u.enabled=1 AND s.revoked_at IS NULL AND s.idle_expires_at>? AND s.absolute_expires_at>?`, input.actor.UserID, input.actor.SessionID, now, now).Scan(&role, &until, &input.actor.Username, &input.actor.SessionPrefix)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAcceleratorForbidden
	}
	if err != nil {
		return err
	}
	if !acceleratorRoleAllowed(role, input) {
		return ErrAcceleratorForbidden
	}
	if input.write && (!until.Valid || !now.Before(until.Time)) {
		return ErrAdminMutationRecent
	}
	return acceleratorScopeExists(ctx, db, input.scope)
}

func acceleratorRoleAllowed(role string, input acceleratorAuthorization) bool {
	if role == "owner" {
		return true
	}
	return role == "admin" && input.scope.Kind == "node"
}

func validateAcceleratorAuthority(input acceleratorAuthorization) error {
	if input.actor == nil || (input.write && input.actor.RequestID == "") {
		return ErrAcceleratorForbidden
	}
	if input.scope.Kind != "node" && input.scope.Kind != "enrollment_policy" {
		return ErrAcceleratorPolicy
	}
	if input.scope.Kind == "enrollment_policy" && input.scope.ID != "default" {
		return ErrAcceleratorPolicy
	}
	return nil
}

func acceleratorScopeExists(ctx context.Context, db schemaExecutor, scope AcceleratorPolicyScope) error {
	if scope.Kind == "node" {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id=? AND `+hiddenNodeFilter,
			scope.ID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return sql.ErrNoRows
		}
	}
	return nil
}
