package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var (
	ErrThemeForbidden  = errors.New("store: theme owner session is required")
	ErrThemeRecentAuth = errors.New("store: theme operation requires recent authentication")
)

// ThemeActor 由 HTTP 边界提供；事务中重查账号、会话及最近认证。
type ThemeActor DashboardActor

type themeActorKey struct{}

func WithThemeActor(ctx context.Context, actor ThemeActor) context.Context {
	return context.WithValue(ctx, themeActorKey{}, actor)
}

type themeWrite struct {
	action, resourceType, resourceID string
}

type themeTransaction struct {
	ctx       context.Context
	tx        *sql.Tx
	authority dashboardAuthority
}

func (store *ThemeStore) mutate(ctx context.Context, write themeWrite, apply func(themeTransaction) error) error {
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	authority, err := themeAuthority(ctx, tx)
	if err != nil {
		return err
	}
	mutation := themeTransaction{ctx: ctx, tx: tx, authority: authority}
	if err := apply(mutation); err != nil {
		return err
	}
	if err := mutation.audit(write); err != nil {
		return err
	}
	return tx.Commit()
}

func themeAuthority(ctx context.Context, tx *sql.Tx) (dashboardAuthority, error) {
	actor, exists := ctx.Value(themeActorKey{}).(ThemeActor)
	if !exists {
		// 无 HTTP 主体的服务内恢复流程明确记为 system。
		return dashboardAuthority{username: "system", now: model.NowUTC()}, nil
	}
	authority, err := dashboardActorAuthority(ctx, tx, DashboardActor(actor))
	if errors.Is(err, ErrDashboardForbidden) {
		return authority, ErrThemeForbidden
	}
	if err != nil {
		return authority, err
	}
	if authority.role != model.AdminRoleOwner {
		return authority, ErrThemeForbidden
	}
	if !authority.reauthenticated.Valid || !authority.now.Before(authority.reauthenticated.Time) {
		return authority, ErrThemeRecentAuth
	}
	return authority, nil
}

func (mutation themeTransaction) audit(write themeWrite) error {
	authority, actor := mutation.authority, mutation.authority.actor
	return insertAuditEvent(mutation.ctx, mutation.tx, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: dashboardActorField(actor.RequestID, 128), ActorID: actor.UserID,
		ActorUsername: authority.username, SessionPrefix: authority.sessionPrefix,
		Action: write.action, ResourceType: write.resourceType, ResourceID: write.resourceID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", CreatedAt: authority.now,
		IPAddress: dashboardActorField(actor.IPAddress, 128), UserAgent: dashboardActorField(actor.UserAgent, 512),
	})
}

// ThemeMarketBinding 将下载时的可信源绑定到最终安装事务，防止并发吊销失效。
type ThemeMarketBinding struct {
	SourceID, CatalogURL, PublicKey, CatalogDigest string
	ExpiresAt                                      time.Time
	Now                                            func() time.Time
}

type themeMarketBindingKey struct{}

func WithThemeMarketBinding(ctx context.Context, binding ThemeMarketBinding) context.Context {
	return context.WithValue(ctx, themeMarketBindingKey{}, binding)
}

func (mutation themeTransaction) validateMarketBinding() error {
	binding, exists := mutation.ctx.Value(themeMarketBindingKey{}).(ThemeMarketBinding)
	if !exists {
		return nil
	}
	now := model.NowUTC()
	if binding.Now != nil {
		now = binding.Now()
	}
	if !now.Before(binding.ExpiresAt) {
		return ErrThemeMarketSourceID
	}
	source, err := scanThemeMarket(mutation.tx.QueryRowContext(mutation.ctx,
		"SELECT "+themeMarketColumns+" FROM theme_market_sources WHERE id=?", binding.SourceID))
	if err != nil {
		return err
	}
	if source == nil || !source.Enabled || source.PublicKey != binding.PublicKey || source.CatalogURL != binding.CatalogURL ||
		source.CatalogDigest != binding.CatalogDigest {
		return ErrThemeMarketSourceID
	}
	return nil
}
