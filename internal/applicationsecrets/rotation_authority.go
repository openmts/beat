package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrRotationForbidden  = errors.New("active owner session is required for key rotation")
	ErrRotationRecentAuth = errors.New("recent authentication is required for key rotation")
)

// RotationActor 只由认证后的 HTTP 上下文生成，不能从请求 JSON 接受。
type RotationActor struct{ UserID, SessionID, RequestID, IPAddress, UserAgent string }

type rotationAuthority struct {
	actor            RotationActor
	username, prefix string
}

func (service *Service) authorizeRotation(ctx context.Context, tx *sql.Tx, actor *RotationActor) (*rotationAuthority, error) {
	if actor == nil {
		return nil, nil
	}
	if actor.UserID == "" || actor.SessionID == "" {
		return nil, ErrRotationForbidden
	}
	authority := &rotationAuthority{actor: *actor}
	var revoked, recent sql.NullTime
	var idle, absolute time.Time
	err := tx.QueryRowContext(ctx, `SELECT users.username, sessions.token_prefix, sessions.revoked_at,
		sessions.idle_expires_at, sessions.absolute_expires_at, sessions.reauthenticated_until
		FROM admin_sessions AS sessions JOIN admin_users AS users ON users.id = sessions.user_id
		WHERE sessions.id = ? AND users.id = ? AND users.enabled = 1 AND users.role = 'owner'`, actor.SessionID, actor.UserID).Scan(
		&authority.username, &authority.prefix, &revoked, &idle, &absolute, &recent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRotationForbidden
	}
	if err != nil {
		return nil, err
	}
	now := service.now()
	if revoked.Valid || !now.Before(idle) || !now.Before(absolute) {
		return nil, ErrRotationForbidden
	}
	if !recent.Valid || !now.Before(recent.Time) {
		return nil, ErrRotationRecentAuth
	}
	return authority, nil
}

type rotationAudit struct {
	authority  *rotationAuthority
	id         string
	generation int64
}

type rotationCommit struct {
	actor      *RotationActor
	id         string
	generation int64
}

func (service *Service) commitRotation(ctx context.Context, tx *sql.Tx, commit rotationCommit) error {
	if err := service.checkpoint("rotation_before_activate"); err != nil {
		return err
	}
	// 库内角色不能越过写事务改变，但时间型授权仍须在提交前重新核对。
	authority, err := service.authorizeRotation(ctx, tx, commit.actor)
	if err != nil {
		return err
	}
	if err := service.auditRotation(ctx, tx, rotationAudit{authority: authority, id: commit.id, generation: commit.generation}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		service.unhealthy.Store(true)
		return err
	}
	return nil
}

func (service *Service) auditRotation(ctx context.Context, tx *sql.Tx, audit rotationAudit) error {
	if audit.authority == nil {
		return nil
	}
	authority := audit.authority
	detail := fmt.Sprintf(`{"generation":%d,"phase":"accepted"}`, audit.generation)
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_audit_events
		(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
		VALUES(?,?,?,?,'application_secret.rotation.start','application_secret_rotation',?,'success',?,?,?,?,?)`, audit.id,
		authority.actor.RequestID, authority.actor.UserID, authority.username, audit.id, detail, authority.actor.IPAddress, authority.actor.UserAgent, authority.prefix, service.now())
	return requireOne(result, err)
}
