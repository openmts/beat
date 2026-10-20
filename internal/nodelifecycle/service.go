package nodelifecycle

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/notificationqueue"
)

var (
	ErrForbidden   = errors.New("node lifecycle permission denied")
	ErrUnavailable = errors.New("node lifecycle unavailable")
	ErrNotFound    = errors.New("node lifecycle not found")
	ErrConflict    = errors.New("node lifecycle changed")
)

type Actor struct{ UserID, SessionID, RequestID string }

type Queue interface {
	MutateLifecycle(context.Context, notificationqueue.LifecycleMutation) (notificationqueue.Receipt, error)
}

type Config struct {
	Database *sql.DB
	Queue    Queue
	Now      func() time.Time
}

type Service struct {
	observer func(error)
	db       *sql.DB
	queue    Queue
	now      func() time.Time
}

type transaction struct {
	ctx     context.Context
	db      *sql.Tx
	now     time.Time
	eventID string
}

func NewService(config Config) (*Service, error) {
	if config.Database == nil || config.Queue == nil {
		return nil, ErrUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: config.Database, queue: config.Queue, now: config.Now}, nil
}

func (s *Service) write(ctx context.Context, action func(*transaction) (*notificationqueue.Request, error)) error {
	var state *transaction
	_, err := s.queue.MutateLifecycle(ctx, notificationqueue.LifecycleMutation{
		Apply: func(db *sql.Tx) (*notificationqueue.Request, error) {
			state = &transaction{ctx: ctx, db: db, now: s.now()}
			return action(state)
		},
		Complete: func(db *sql.Tx, receipt notificationqueue.Receipt) error {
			if state == nil || state.eventID == "" || receipt.Skipped {
				return nil
			}
			_, err := db.ExecContext(ctx, `UPDATE node_lifecycle_events SET message_id=?,routing_status=? WHERE id=?`, receipt.MessageID, receipt.RoutingState, state.eventID)
			return err
		},
	})
	return err
}

func (tx *transaction) authorize(actor Actor, owner, recent bool) error {
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

func (tx *transaction) audit(actor Actor, action, id string) error {
	var actorID any
	if actor.UserID != "scheduler" {
		actorID = actor.UserID
	}
	_, err := tx.db.ExecContext(tx.ctx, `INSERT INTO admin_audit_events(id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
 VALUES(?,?,?,COALESCE((SELECT username FROM admin_users WHERE id=?),'scheduler'),?,'node_lifecycle',?,'success','{}','','','',?)`, uuid.NewString(), actor.RequestID, actorID, actor.UserID, action, id, tx.now)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// SetObserver 必须在启动Run前调用；回调不应阻塞后台处理。
func (s *Service) SetObserver(observer func(error)) { s.observer = observer }

func (s *Service) observe(err error) {
	if s.observer != nil {
		s.observer(err)
	}
}
