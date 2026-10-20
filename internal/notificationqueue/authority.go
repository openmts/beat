package notificationqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type authorityRule struct {
	actor         Actor
	owner, recent bool
}

type authority struct {
	actor            Actor
	username, prefix string
}

func (service *Service) authorize(ctx context.Context, database queryStore, rule authorityRule) (authority, error) {
	if rule.actor.UserID == "" || rule.actor.SessionID == "" {
		return authority{}, ErrForbidden
	}
	var role string
	var enabled bool
	var idle, absolute time.Time
	var revoked, recent sql.NullTime
	actor := authority{actor: rule.actor}
	err := database.QueryRowContext(ctx, `SELECT u.username,u.role,u.enabled,s.token_prefix,s.revoked_at,s.idle_expires_at,s.absolute_expires_at,s.reauthenticated_until
		FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id WHERE u.id=? AND s.id=?`, rule.actor.UserID, rule.actor.SessionID).Scan(&actor.username, &role, &enabled, &actor.prefix, &revoked, &idle, &absolute, &recent)
	if errors.Is(err, sql.ErrNoRows) {
		return authority{}, ErrForbidden
	}
	if err != nil {
		return authority{}, ErrQueueUnavailable
	}
	now := service.now()
	if !enabled || revoked.Valid || !now.Before(idle) || !now.Before(absolute) || rule.owner && role != "owner" {
		return authority{}, ErrForbidden
	}
	if rule.recent && (!recent.Valid || !now.Before(recent.Time)) {
		return authority{}, ErrRecentAuth
	}
	return actor, nil
}

type auditInput struct {
	rule                             authorityRule
	action, resourceType, resourceID string
	detail                           map[string]any
}

func (service *Service) audit(ctx context.Context, tx *sql.Tx, input auditInput) error {
	actor, err := service.authorize(ctx, tx, input.rule)
	if err != nil {
		return err
	}
	id, err := service.newID()
	if err != nil {
		return err
	}
	detail, err := json.Marshal(input.detail)
	if err != nil {
		return ErrInvalidRequest
	}
	requestID := actor.actor.RequestID
	if requestID == "" {
		requestID = id
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_audit_events
		(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
		VALUES(?,?,?,?,?,?,?,'success',?,?,?,?,?)`, id, requestID, actor.actor.UserID, actor.username, input.action, input.resourceType, input.resourceID,
		string(detail), actor.actor.IPAddress, actor.actor.UserAgent, actor.prefix, service.now())
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}
