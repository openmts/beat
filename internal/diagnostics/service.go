package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

// 诊断控制面的固定错误域：调用方必须区分不可用、被拒绝与状态冲突。
var (
	ErrUnavailable = errors.New("diagnostic service is unavailable")
	ErrForbidden   = errors.New("diagnostic operation is not permitted")
	ErrInvalid     = errors.New("invalid diagnostic request")
	ErrNotFound    = errors.New("diagnostic resource was not found")
	ErrConflict    = errors.New("diagnostic state conflict")
	ErrBudget      = errors.New("diagnostic byte budget is exhausted")
	ErrCapability  = errors.New("diagnostic capability is unavailable")
)

const (
	CapabilityLifetime = 10 * time.Minute
	QueueLifetime      = 24 * time.Hour
	resourceType       = "diagnostic"
)

// Metrics 是诊断数值的唯一存储边界；SQLite 不保存任何数值结果。
type Metrics interface {
	WriteDiagnosticSamples(context.Context, store.DiagnosticSampleBatch) error
	ReadDiagnosticPoints(context.Context, store.DiagnosticQuery) ([]store.TimePoint, error)
	DeleteDiagnosticJob(context.Context, string) error
}

// PeerLabels 提供管理员界面显示所需的节点名称；ID 从不作为提交值。
type PeerLabels interface {
	NodeName(context.Context, string) (string, error)
}

type Config struct {
	Database *sql.DB
	Secrets  secretbox.ResourceService
	Metrics  Metrics
	Labels   PeerLabels
	Resolve  func(context.Context, string) ([]netip.Addr, error)
	Enabled  bool
	Now      func() time.Time
}

type Service struct {
	db      *sql.DB
	secrets secretbox.ResourceService
	metrics Metrics
	labels  PeerLabels
	resolve func(context.Context, string) ([]netip.Addr, error)
	enabled bool
	now     func() time.Time
}

// Actor 是发起管理操作的会话主体。
type Actor struct {
	UserID        string
	Username      string
	Role          string
	SessionID     string
	SessionPrefix string
	RequestID     string
	IPAddress     string
	UserAgent     string
}

// NodeProof 是 Agent 的每节点凭据证明。
type NodeProof struct{ Token string }

type transaction struct {
	authority *Actor
	ctx       context.Context
	tx        *sql.Tx
	access    secretbox.ResourceAccess
	now       time.Time
}

func NewService(config Config) (*Service, error) {
	if config.Database == nil || config.Secrets == nil || config.Metrics == nil {
		return nil, ErrUnavailable
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: config.Database, secrets: config.Secrets, metrics: config.Metrics,
		labels: config.Labels, resolve: config.Resolve, enabled: config.Enabled, now: config.Now}, nil
}

// Enabled 报告诊断是否在部署层启用；未启用时任何创建与租约都不可用。
func (service *Service) Enabled() bool { return service != nil && service.enabled }

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

func (service *Service) read(ctx context.Context, query func(transaction) error) error {
	tx, err := service.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	if err := query(service.transaction(ctx, tx, nil)); err != nil {
		return err
	}
	return tx.Commit()
}

func (service *Service) transaction(ctx context.Context, db *sql.Tx, access secretbox.ResourceAccess) transaction {
	return transaction{ctx: ctx, tx: db, access: access, now: service.now(), authority: &Actor{}}
}

func (tx transaction) exec(query string, args ...any) error {
	_, err := tx.tx.ExecContext(tx.ctx, query, args...)
	if err != nil {
		return classifyWriteError(err)
	}
	return nil
}

// one 用于必须精确影响一行的状态迁移。
func (tx transaction) one(query string, args ...any) error {
	result, err := tx.tx.ExecContext(tx.ctx, query, args...)
	if err != nil {
		return classifyWriteError(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func classifyWriteError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	default:
		return ErrUnavailable
	}
}

// authorize 校验会话主体、角色与（可选的）最近重认证状态。
func (tx transaction) authorize(actor Actor, owner, recent bool) error {
	var role string
	var until sql.NullTime
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT u.role,s.reauthenticated_until,u.username,s.token_prefix FROM admin_sessions s
		JOIN admin_users u ON u.id=s.user_id WHERE s.id=? AND u.id=? AND u.enabled=1
		AND s.revoked_at IS NULL AND s.idle_expires_at>? AND s.absolute_expires_at>?`,
		actor.SessionID, actor.UserID, tx.now, tx.now).Scan(&role, &until, &actor.Username, &actor.SessionPrefix)
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

// node 解析每节点 Agent 凭据；吊销或未知凭据都不能通过。
func (tx transaction) node(proof NodeProof) (string, error) {
	if _, valid := agentcredential.DisplayPrefix(proof.Token); !valid {
		return "", ErrForbidden
	}
	digest := agentcredential.Digest(proof.Token)
	var id string
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT id FROM nodes WHERE agent_token_hash=? AND
		agent_token_created_at IS NOT NULL AND agent_token_revoked_at IS NULL`, digest[:]).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", ErrUnavailable
	}
	return id, nil
}

type diagnosticAudit struct {
	actorID, action, id, outcome string
}

func (tx transaction) audit(actorID, action, id string) error {
	return tx.auditEvent(diagnosticAudit{actorID: actorID, action: action, id: id, outcome: "success"})
}

func (tx transaction) auditEvent(event diagnosticAudit) error {
	actor := Actor{}
	if tx.authority != nil {
		actor = *tx.authority
	}
	return tx.one(`INSERT INTO admin_audit_events
 (id,request_id,actor_id,actor_username,action,resource_type,resource_id,outcome,detail_json,ip_address,user_agent,session_prefix,created_at)
 VALUES(?,?,(SELECT id FROM admin_users WHERE id=?),?,?,'diagnostic',?,?,'{}',?,?,?,?)`, uuid.NewString(), actor.RequestID,
		event.actorID, actor.Username, event.action, event.id, event.outcome, actor.IPAddress, actor.UserAgent, actor.SessionPrefix, tx.now)
}
