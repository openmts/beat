package serviceprincipal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

var (
	// ErrPrincipalNotFound 表示目标服务令牌不存在。
	ErrPrincipalNotFound = errors.New("service token not found")
	// ErrPrincipalRevoked 表示目标服务令牌已被撤销。
	ErrPrincipalRevoked = errors.New("service token is already revoked")
	// ErrRotationConflict 表示轮换请求与当前状态冲突。
	ErrRotationConflict = errors.New("service token rotation conflict")
	// ErrStorage 表示持久化失败，调用方不应据此判断令牌状态。
	ErrStorage = errors.New("service token storage failure")
)

// Store 是服务令牌管理所需的最小持久化接口。
type Store interface {
	Create(context.Context, model.ServicePrincipal, store.ServicePrincipalActor) error
	List(context.Context) ([]model.ServicePrincipal, error)
	Revoke(context.Context, store.ServicePrincipalRevocation) error
	Rotate(context.Context, store.ServicePrincipalRotation) error
	RevokeAll(context.Context, store.ServicePrincipalBulkRevocation) (int, error)
	Expire(context.Context, time.Time) (int, error)
}

// Service 负责策略校验、令牌生成与管理流程编排。
type Service struct {
	store Store
	now   func() time.Time
	ids   func() string
}

// NewService 创建服务令牌管理服务。
func NewService(storage Store) *Service {
	return &Service{store: storage, now: func() time.Time { return time.Now().UTC() }, ids: uuid.NewString}
}

// CreateInput 是一次创建请求的原始输入。
type CreateInput struct {
	Name        string
	Purpose     string
	Scopes      []model.ServiceScope
	Resources   []model.ServiceResource
	Visibility  string
	SourceCIDRs []string
	ExpiresAt   time.Time
	Extended    bool
}

// RotateInput 是一次轮换请求的原始输入，包含新的策略与重叠窗口。
type RotateInput struct {
	Name        string
	Purpose     string
	Scopes      []model.ServiceScope
	Resources   []model.ServiceResource
	Visibility  string
	SourceCIDRs []string
	ExpiresAt   time.Time
	Extended    bool
	Overlap     time.Duration
}

// RevokeInput 是一次撤销请求的原始输入。
type RevokeInput struct {
	ID     string
	Reason string
}

// Credential 携带一次性明文令牌与可安全展示的主体元数据。
type Credential struct {
	Token     string                 `json:"token"`
	Principal model.ServicePrincipal `json:"principal"`
}

// Create 校验策略、生成令牌并持久化主体。
func (service *Service) Create(ctx context.Context, actor store.ServicePrincipalActor, input CreateInput) (Credential, error) {
	now := service.now()
	policy, err := IssueRequest(input).Validate(now)
	if err != nil {
		return Credential{}, err
	}
	if actor.UserID == "" {
		return Credential{}, ErrStorage
	}
	token, err := Generate()
	if err != nil {
		return Credential{}, err
	}
	value := model.ServicePrincipal{
		ID: service.ids(), Name: policy.Name, Purpose: policy.Purpose, TokenPrefix: token.Prefix,
		TokenHash: token.Hash, Visibility: policy.Visibility, SourceCIDRs: policy.SourceCIDRs,
		Scopes: policy.Scopes, Resources: policy.Resources, ExpiresAt: policy.ExpiresAt,
		Revision: 1, CreatedBy: actor.UserID, CreatedAt: now,
	}
	if err := service.store.Create(ctx, value, actor); err != nil {
		return Credential{}, classifyStoreError(err)
	}
	return Credential{Token: token.Plaintext, Principal: value}, nil
}

// Rotate 校验新策略、生成后继令牌并按重叠窗口作废前任。
func (service *Service) Rotate(ctx context.Context, actor store.ServicePrincipalActor, previous string, input RotateInput) (Credential, error) {
	now := service.now()
	policy, err := IssueRequest{Name: input.Name, Purpose: input.Purpose, Scopes: input.Scopes,
		Resources: input.Resources, Visibility: input.Visibility, SourceCIDRs: input.SourceCIDRs,
		ExpiresAt: input.ExpiresAt, Extended: input.Extended}.Validate(now)
	if err != nil {
		return Credential{}, err
	}
	if actor.UserID == "" {
		return Credential{}, ErrStorage
	}
	token, err := Generate()
	if err != nil {
		return Credential{}, err
	}
	next := model.ServicePrincipal{
		ID: service.ids(), Name: policy.Name, Purpose: policy.Purpose, TokenPrefix: token.Prefix,
		TokenHash: token.Hash, Visibility: policy.Visibility, SourceCIDRs: policy.SourceCIDRs,
		Scopes: policy.Scopes, Resources: policy.Resources, ExpiresAt: policy.ExpiresAt,
		RotationOverlapSeconds: int(input.Overlap.Seconds()), RotatedFrom: previous,
		Revision: 1, CreatedBy: actor.UserID, CreatedAt: now,
	}
	change := store.ServicePrincipalRotation{Previous: previous, Next: next, Overlap: input.Overlap, At: now, Actor: actor}
	if err := service.store.Rotate(ctx, change); err != nil {
		return Credential{}, classifyStoreError(err)
	}
	return Credential{Token: token.Plaintext, Principal: next}, nil
}

// List 返回管理视图，摘要不会离开服务端。
func (service *Service) List(ctx context.Context) ([]model.ServicePrincipal, error) {
	values, err := service.store.List(ctx)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	for index := range values {
		values[index].TokenHash = nil
	}
	return values, nil
}

// Revoke 撤销一个活动令牌。
func (service *Service) Revoke(ctx context.Context, actor store.ServicePrincipalActor, input RevokeInput) error {
	if input.ID == "" {
		return ErrPrincipalNotFound
	}
	if len(input.Reason) > model.MaximumServiceTokenPurpose {
		return ErrPurpose
	}
	change := store.ServicePrincipalRevocation{ID: input.ID, Reason: input.Reason, At: service.now(), Actor: actor}
	return classifyStoreError(service.store.Revoke(ctx, change))
}

// RevokeAll 在恢复或回滚后统一作废全部令牌。
func (service *Service) RevokeAll(ctx context.Context, actor store.ServicePrincipalActor, reason string) (int, error) {
	change := store.ServicePrincipalBulkRevocation{Reason: reason, At: service.now(), Actor: actor}
	revoked, err := service.store.RevokeAll(ctx, change)
	return revoked, classifyStoreError(err)
}

// Expire 撤销已过期的令牌，供启动时调用。
func (service *Service) Expire(ctx context.Context, now time.Time) (int, error) {
	revoked, err := service.store.Expire(ctx, now)
	return revoked, classifyStoreError(err)
}

func classifyStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, store.ErrServicePrincipalNotFound):
		return ErrPrincipalNotFound
	case errors.Is(err, store.ErrServicePrincipalRevoked):
		return ErrPrincipalRevoked
	case errors.Is(err, store.ErrServicePrincipalRotation):
		return ErrRotationConflict
	default:
		return ErrStorage
	}
}
