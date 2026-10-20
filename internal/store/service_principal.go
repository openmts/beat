package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

var (
	// ErrServicePrincipalNotFound 表示服务令牌主体不存在。
	ErrServicePrincipalNotFound = errors.New("service token not found")
	// ErrServicePrincipalRevoked 表示服务令牌主体已被撤销。
	ErrServicePrincipalRevoked = errors.New("service token is already revoked")
	// ErrServicePrincipalRotation 表示轮换请求违反重叠窗口或前置状态约束。
	ErrServicePrincipalRotation = errors.New("service token rotation is invalid")
)

const servicePrincipalColumns = `id, name, purpose, token_prefix, token_hash, visibility, source_cidrs,
	expires_at, revoked_at, revoked_reason, superseded_at, rotation_overlap_seconds,
	COALESCE(rotated_from, ''), revision, created_by, created_at, last_used_at`

// ServicePrincipalActor 是触发服务令牌变更的已认证管理员身份。
type ServicePrincipalActor struct {
	UserID    string
	SessionID string
	RequestID string
	IPAddress string
	UserAgent string
}

// ServicePrincipalRevocation 描述一次显式撤销。
type ServicePrincipalRevocation struct {
	ID     string
	Reason string
	At     time.Time
	Actor  ServicePrincipalActor
}

// ServicePrincipalRotation 描述一次带重叠窗口的轮换。
type ServicePrincipalRotation struct {
	Previous string
	Next     model.ServicePrincipal
	Overlap  time.Duration
	At       time.Time
	Actor    ServicePrincipalActor
}

// ServicePrincipalBulkRevocation 描述恢复或回滚后的统一作废。
type ServicePrincipalBulkRevocation struct {
	Reason string
	At     time.Time
	Actor  ServicePrincipalActor
}

// ServicePrincipalStore 持久化服务令牌主体，只保存摘要与授权元数据。
type ServicePrincipalStore struct{ db *sql.DB }

// NewServicePrincipalStore 创建服务令牌存储。
func NewServicePrincipalStore(db *sql.DB) *ServicePrincipalStore {
	return &ServicePrincipalStore{db: db}
}

// Create 在同一事务内写入主体、约束与成功审计。
func (service *ServicePrincipalStore) Create(ctx context.Context, value model.ServicePrincipal, actor ServicePrincipalActor) error {
	if err := validateServicePrincipal(value); err != nil {
		return err
	}
	return withServicePrincipalWrite(ctx, service.db, "create service token", func(tx *sql.Tx) error {
		if err := insertServicePrincipal(ctx, tx, value); err != nil {
			return err
		}
		return servicePrincipalAudit(ctx, tx, servicePrincipalActionCreate, value.ID, actor, servicePrincipalDetail(&value))
	})
}

// ByHash 按令牌摘要加载主体；不存在时返回 ErrServicePrincipalNotFound。
func (service *ServicePrincipalStore) ByHash(ctx context.Context, hash []byte) (*model.ServicePrincipal, error) {
	if len(hash) != 32 {
		return nil, ErrServicePrincipalNotFound
	}
	row := service.db.QueryRowContext(ctx, "SELECT "+servicePrincipalColumns+" FROM service_principals WHERE token_hash = ?", hash)
	value, err := scanServicePrincipal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrServicePrincipalNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := service.loadConstraints(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

// List 返回全部服务令牌元数据，按创建时间与标识稳定排序。
func (service *ServicePrincipalStore) List(ctx context.Context) ([]model.ServicePrincipal, error) {
	rows, err := service.db.QueryContext(ctx, "SELECT "+servicePrincipalColumns+
		" FROM service_principals ORDER BY created_at, id")
	if err != nil {
		return nil, fmt.Errorf("list service tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.ServicePrincipal{}
	for rows.Next() {
		value, err := scanServicePrincipal(rows)
		if err != nil {
			return nil, err
		}
		if err := service.loadConstraints(ctx, value); err != nil {
			return nil, err
		}
		values = append(values, *value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service tokens: %w", err)
	}
	return values, nil
}

// Revoke 撤销一个活动令牌，并按同一事务记录审计。
func (service *ServicePrincipalStore) Revoke(ctx context.Context, change ServicePrincipalRevocation) error {
	if change.ID == "" {
		return ErrServicePrincipalNotFound
	}
	return withServicePrincipalWrite(ctx, service.db, "revoke service token", func(tx *sql.Tx) error {
		name, prefix, err := readServicePrincipalIdentity(ctx, tx, change.ID)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE service_principals
			SET revoked_at = ?, revoked_reason = ?, revision = revision + 1
			WHERE id = ? AND revoked_at IS NULL`, change.At.UTC(), change.Reason, change.ID)
		if err != nil {
			return fmt.Errorf("revoke service token: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("revoke service token: %w", err)
		}
		if affected != 1 {
			return classifyServicePrincipalMutation(ctx, tx, change.ID)
		}
		detail := servicePrincipalAuditDetail{Name: name, Prefix: prefix, Reason: change.Reason}
		return servicePrincipalAudit(ctx, tx, servicePrincipalActionRevoke, change.ID, change.Actor, detail)
	})
}

// Rotate 创建后继令牌，并按重叠窗口把前任标记为待失效。
func (service *ServicePrincipalStore) Rotate(ctx context.Context, change ServicePrincipalRotation) error {
	if change.Overlap < 0 || change.Overlap > model.MaximumServiceTokenRotationOverlap {
		return ErrServicePrincipalRotation
	}
	change.Next.RotatedFrom = change.Previous
	if err := validateServicePrincipal(change.Next); err != nil {
		return err
	}
	return withServicePrincipalWrite(ctx, service.db, "rotate service token", func(tx *sql.Tx) error {
		if err := rotateServicePrincipal(ctx, tx, change); err != nil {
			return err
		}
		detail := servicePrincipalDetail(&change.Next)
		detail.Reason = "rotation"
		detail.RotatedFrom = change.Previous
		return servicePrincipalAudit(ctx, tx, servicePrincipalActionRotate, change.Next.ID, change.Actor, detail)
	})
}

// RecordUse 只向前推进最近使用时间，避免并发请求回退时间戳。
func (service *ServicePrincipalStore) RecordUse(ctx context.Context, id string, at time.Time) error {
	if id == "" {
		return ErrServicePrincipalNotFound
	}
	_, err := service.db.ExecContext(ctx, `UPDATE service_principals SET last_used_at = ?
		WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`, at.UTC(), id, at.UTC())
	if err != nil {
		return fmt.Errorf("record service token use: %w", err)
	}
	return nil
}

// RevokeAll 在恢复或回滚后撤销全部服务令牌，并记录一次汇总审计。
func (service *ServicePrincipalStore) RevokeAll(ctx context.Context, change ServicePrincipalBulkRevocation) (int, error) {
	revoked := 0
	err := withServicePrincipalWrite(ctx, service.db, "revoke all service tokens", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE service_principals
			SET revoked_at = ?, revoked_reason = ?, revision = revision + 1
			WHERE revoked_at IS NULL`, change.At.UTC(), change.Reason)
		if err != nil {
			return fmt.Errorf("revoke all service tokens: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("revoke all service tokens: %w", err)
		}
		revoked = int(affected)
		if revoked == 0 {
			return nil
		}
		detail := servicePrincipalAuditDetail{Reason: change.Reason, Revoked: revoked}
		return servicePrincipalAudit(ctx, tx, servicePrincipalActionRevokeAll, "service_tokens", change.Actor, detail)
	})
	if err != nil {
		return 0, err
	}
	return revoked, nil
}

// Expire 撤销绝对有效期已过或轮换重叠窗口已结束的令牌，并记录汇总审计。
func (service *ServicePrincipalStore) Expire(ctx context.Context, now time.Time) (int, error) {
	stamp := now.UTC()
	revoked := 0
	err := withServicePrincipalWrite(ctx, service.db, "expire service tokens", func(tx *sql.Tx) error {
		expired, err := expireByDeadline(ctx, tx, stamp)
		if err != nil {
			return err
		}
		rotated, err := expireSuperseded(ctx, tx, stamp)
		if err != nil {
			return err
		}
		revoked = expired + rotated
		if revoked == 0 {
			return nil
		}
		detail := servicePrincipalAuditDetail{Revoked: revoked, Reason: "expired"}
		return servicePrincipalAudit(ctx, tx, servicePrincipalActionExpire, "service_tokens", ServicePrincipalActor{}, detail)
	})
	if err != nil {
		return 0, err
	}
	return revoked, nil
}

func withServicePrincipalWrite(ctx context.Context, db *sql.DB, action string, write func(*sql.Tx) error) error {
	tx, err := beginWriteTx(ctx, db)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := write(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}
