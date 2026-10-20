package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

type Config struct {
	Spool    *Spool
	Queue    CompletionQueue
	Database *sql.DB
	Secrets  secretbox.ResourceService
	Enabled  bool
	Now      func() time.Time
}

type Service struct {
	scanMu  sync.Mutex
	scans   map[string]context.CancelFunc
	scanWG  sync.WaitGroup
	closed  bool
	spool   *Spool
	queue   CompletionQueue
	db      *sql.DB
	secrets secretbox.ResourceService
	enabled bool
	now     func() time.Time
}

type Actor struct {
	RequestID     string `json:"-"`
	IPAddress     string `json:"-"`
	UserAgent     string `json:"-"`
	Username      string `json:"-"`
	SessionPrefix string `json:"-"`
	SessionID     string `json:"-"`
	UserID        string `json:"-"`
}

type transaction struct {
	authority *Actor
	ctx       context.Context
	tx        *sql.Tx
	access    secretbox.ResourceAccess
	now       time.Time
}

func NewService(config Config) (*Service, error) {
	if config.Database == nil || config.Secrets == nil {
		return nil, ErrUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{scans: make(map[string]context.CancelFunc), spool: config.Spool, queue: config.Queue, db: config.Database, secrets: config.Secrets, enabled: config.Enabled, now: config.Now}, nil
}

func (service *Service) write(ctx context.Context, mutate func(transaction) error) error {
	if !service.enabled {
		return ErrForbidden
	}
	return service.atomicWrite(ctx, mutate)
}

func (service *Service) atomicWrite(ctx context.Context, mutate func(transaction) error) error {
	return service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = tx.Rollback() }()
		if err := mutate(service.transaction(ctx, tx, access)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func (tx transaction) authorize(actor Actor, owner, recent bool) error {
	var role string
	var until sql.NullTime
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT u.role,s.reauthenticated_until,u.username,s.token_prefix FROM admin_sessions s
		JOIN admin_users u ON u.id=s.user_id WHERE s.id=? AND u.id=? AND u.enabled=1
		AND s.revoked_at IS NULL AND s.idle_expires_at>? AND s.absolute_expires_at>?`, actor.SessionID, actor.UserID, tx.now, tx.now).Scan(&role, &until, &actor.Username, &actor.SessionPrefix)
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
	if tx.authority != nil {
		*tx.authority = actor
	}
	return nil
}

type operationAudit struct{ actorID, action, id, outcome string }

func (tx transaction) audit(actorID, action, id string) error {
	return tx.auditEvent(operationAudit{actorID: actorID, action: action, id: id, outcome: "success"})
}

func (tx transaction) auditEvent(event operationAudit) error {
	actor := Actor{}
	if tx.authority != nil {
		actor = *tx.authority
	}
	return tx.one(`INSERT INTO admin_audit_events
 (id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
 VALUES(?,?,(SELECT id FROM admin_users WHERE id=?),?,?,'operation',?,?,'{}',?,?,?,?)`, uuid.NewString(), actor.RequestID, event.actorID, actor.Username, event.action, event.id, event.outcome, actor.IPAddress, actor.UserAgent, actor.SessionPrefix, tx.now)
}

func (service *Service) transaction(ctx context.Context, db *sql.Tx, access secretbox.ResourceAccess) transaction {
	return transaction{ctx: ctx, tx: db, access: access, now: service.now(), authority: &Actor{}}
}

func (tx transaction) exec(query string, args ...any) error {
	_, err := tx.tx.ExecContext(tx.ctx, query, args...)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func fingerprint(access secretbox.ResourceAccess, content []byte) (string, error) {
	keyed, ok := access.(secretbox.OperationFingerprinter)
	if !ok {
		return "", ErrUnavailable
	}
	return keyed.OperationFingerprint(content)
}

func (tx transaction) one(query string, args ...any) error {
	result, err := tx.tx.ExecContext(tx.ctx, query, args...)
	if err != nil {
		return ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrUnavailable
	}
	return nil
}
