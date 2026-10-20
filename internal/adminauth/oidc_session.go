package adminauth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// OIDCSessionRequest 仅由已校验签名、issuer、audience、nonce的回调传入。
type OIDCSessionRequest struct {
	Provider             model.OIDCProvider
	Identity             model.ExternalIdentity
	IPAddress, UserAgent string
}

// CreateOIDCSession 仅在会话、登录时间及成功审计全部提交后返回令牌。
func (service *Service) CreateOIDCSession(ctx context.Context, input OIDCSessionRequest) (LoginResult, error) {
	user, err := service.config.Store.GetUserByID(ctx, input.Identity.UserID)
	if err != nil {
		return LoginResult{}, err
	}
	if user == nil || !user.Enabled {
		return LoginResult{}, ErrInvalidCredentials
	}
	result, session, err := service.newSessionWithAttribution(user, input.IPAddress, input.UserAgent, model.AuthMethodOIDC, &input.Identity.ID)
	if err != nil {
		return LoginResult{}, err
	}
	audit := service.auditEvent(ctx, AuditInput{Principal: &result.Principal, Action: "auth.oidc.login", ResourceType: "provider", ResourceID: input.Provider.ID, Outcome: model.AuditOutcomeSuccess, IPAddress: input.IPAddress, UserAgent: input.UserAgent})
	provenance, err := service.loginSecurity(ctx, loginSecurityInput{Principal: result.Principal,
		AuthMethod: model.SecurityAuthMethodOIDC, AuditID: audit.ID, Now: audit.CreatedAt})
	if err != nil {
		return LoginResult{}, err
	}
	admission := store.OIDCSessionAdmission{Provider: input.Provider, Identity: input.Identity, User: *user,
		Session: *session, Audit: audit, Events: provenance.Events, Fingerprints: provenance.Fingerprints}
	err = service.commitOIDCLoginSecurity(ctx, admission, provenance)
	if errors.Is(err, store.ErrAdminSessionNotFound) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	result.Principal.User.LastLoginAt = &result.Principal.Session.CreatedAt
	return result, nil
}

// commitOIDCLoginSecurity 让 OIDC 会话、审计、安全事件与通知出箱在同一事务内提交。
func (service *Service) commitOIDCLoginSecurity(
	ctx context.Context, admission store.OIDCSessionAdmission, provenance loginProvenance,
) error {
	if service.config.Queue == nil || len(provenance.Requests) == 0 {
		return service.config.Store.AdmitOIDCSession(ctx, admission)
	}
	receipts, err := service.config.Queue.EnqueueBatch(ctx, provenance.Requests, func(tx *sql.Tx) error {
		return service.config.Store.AdmitOIDCSessionTx(ctx, tx, admission)
	})
	if err != nil {
		return err
	}
	service.applySecurityReceipts(ctx, provenance.QueuedEventIDs, receipts)
	return nil
}
