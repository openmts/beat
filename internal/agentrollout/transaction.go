package agentrollout

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type transaction struct {
	ctx              context.Context
	db               *sql.Tx
	at               time.Time
	actor            Actor
	username, prefix string
}

func (s *Service) mutate(ctx context.Context, actor Actor, action func(*transaction) error) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	db, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, at: s.now(), actor: actor}
	if err := tx.authorize(); err != nil {
		return err
	}
	if err := action(tx); err != nil {
		return err
	}
	tx.at = s.now()
	if err := tx.authorize(); err != nil {
		return err
	}
	if err := db.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (tx *transaction) authorize() error { return tx.authority(true) }

func (tx *transaction) authority(recent bool) error {
	var role string
	var until sql.NullTime
	err := tx.db.QueryRowContext(tx.ctx, `SELECT u.role,u.username,s.token_prefix,s.reauthenticated_until FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id WHERE u.id=? AND s.id=? AND u.enabled=1 AND s.revoked_at IS NULL AND s.idle_expires_at>? AND s.absolute_expires_at>?`, tx.actor.UserID, tx.actor.SessionID, tx.at, tx.at).Scan(&role, &tx.username, &tx.prefix, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return ErrUnavailable
	}
	if role != "owner" {
		return ErrForbidden
	}
	if recent && (!until.Valid || !tx.at.Before(until.Time)) {
		return ErrRecentAuth
	}
	return nil
}

type audit struct {
	Action, Kind, ID, Reason string
	Count                    int
}

func (tx *transaction) audit(event audit) error {
	detail, err := json.Marshal(struct {
		Reason string `json:"reason,omitempty"`
		Count  int    `json:"target_count,omitempty"`
	}{event.Reason, event.Count})
	if err != nil {
		return ErrUnavailable
	}
	requestID := bounded(tx.actor.RequestID, maximumIdentifierBytes)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	eventID := uuid.NewString()
	result, err := tx.db.ExecContext(tx.ctx, `INSERT INTO admin_audit_events(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at) VALUES(?,?,NULLIF(?,''),?,?,?,?,'success',?,?,?,?,?)`, eventID, requestID, tx.actor.UserID, tx.username, event.Action, event.Kind, event.ID, string(detail), bounded(tx.actor.IPAddress, maximumIdentifierBytes), bounded(tx.actor.UserAgent, maximumUserAgentBytes), tx.prefix, tx.at)
	if err := requireOne(result, err); err != nil {
		return err
	}
	var count int
	err = tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM admin_audit_events WHERE id=? AND action=? AND resource_type=? AND resource_id=? AND detail_json=? AND request_id=? AND COALESCE(actor_id,'')=? AND actor_username=? AND session_prefix=? AND ip_address=? AND user_agent=? AND created_at=?", eventID, event.Action, event.Kind, event.ID, string(detail), requestID, tx.actor.UserID, tx.username, tx.prefix, bounded(tx.actor.IPAddress, maximumIdentifierBytes), bounded(tx.actor.UserAgent, maximumUserAgentBytes), tx.at).Scan(&count)
	if err != nil || count != 1 {
		return ErrUnavailable
	}
	return nil
}

func bounded(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) > maximum {
		return strings.ToValidUTF8(value[:maximum], "")
	}
	return value
}

func requireOne(result sql.Result, err error) error {
	if err != nil {
		return ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrUnavailable
	}
	return nil
}

func (tx *transaction) exec(query string, args ...any) error {
	_, err := tx.db.ExecContext(tx.ctx, query, args...)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
