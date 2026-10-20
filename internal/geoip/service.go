package geoip

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

type Actor struct{ UserID, SessionID, RequestID string }

type Config struct {
	Database     *sql.DB
	Secrets      secretbox.ResourceService
	Directory    string
	ExternalMMDB string
	Now          func() time.Time
}

type Service struct {
	observer            func(error)
	policyMu            sync.RWMutex
	assetMu             sync.Mutex
	db                  *sql.DB
	secrets             secretbox.ResourceService
	now                 func() time.Time
	directory, external string
	mmdb                *Database
	client              *http.Client
	rateMu              sync.Mutex
	nextRemote          time.Time
}

func NewService(config Config) (*Service, error) {
	if config.Database == nil || config.Secrets == nil {
		return nil, ErrUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: config.Database, secrets: config.Secrets, now: config.Now, directory: config.Directory, external: config.ExternalMMDB, mmdb: &Database{}, client: providerClient()}, nil
}

type transaction struct {
	ctx    context.Context
	db     *sql.Tx
	access secretbox.ResourceAccess
	now    time.Time
}

func (s *Service) write(ctx context.Context, action func(transaction) error) error {
	return s.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		if err = action(transaction{ctx: ctx, db: tx, access: access, now: s.now()}); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func (tx transaction) authorize(actor Actor, owner, recent bool) error {
	var role string
	var until sql.NullTime
	err := tx.db.QueryRowContext(tx.ctx, `SELECT u.role,s.reauthenticated_until FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id
 WHERE u.id=? AND s.id=? AND u.enabled=1 AND s.revoked_at IS NULL AND s.idle_expires_at>? AND s.absolute_expires_at>?`, actor.UserID, actor.SessionID, tx.now, tx.now).Scan(&role, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return ErrUnavailable
	}
	if role != "owner" && (owner || role != "admin") {
		return ErrForbidden
	}
	if recent && (!until.Valid || !tx.now.Before(until.Time)) {
		return ErrForbidden
	}
	return nil
}

func (tx transaction) audit(actor Actor, action, id string) error {
	_, err := tx.db.ExecContext(tx.ctx, `INSERT INTO admin_audit_events(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
 VALUES(?,?,?,(SELECT username FROM admin_users WHERE id=?),?,'geoip',?,'success','{}','','','',?)`, uuid.NewString(), actor.RequestID, actor.UserID, actor.UserID, action, id, tx.now)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) Close() error { return s.mmdb.Close() }

// SetObserver 必须在启动Run前调用；回调不应阻塞后台处理。
func (s *Service) SetObserver(observer func(error)) { s.observer = observer }

func (s *Service) observe(err error) {
	if s.observer != nil {
		s.observer(err)
	}
}
