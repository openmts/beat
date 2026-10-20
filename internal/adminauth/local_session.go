package adminauth

import (
	"context"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// admitLocalSession 原子提交会话、审计、安全事件与通知出箱；失败时不返回可用令牌。
func (service *Service) admitLocalSession(ctx context.Context, result LoginResult, bootstrap bool) error {
	p := result.Principal
	input := AuditInput{Principal: &p, Action: "auth.login", ResourceType: "session", ResourceID: p.Session.ID,
		Outcome: model.AuditOutcomeSuccess, IPAddress: p.Session.IPAddress, UserAgent: p.Session.UserAgent}
	method := model.SecurityAuthMethodPassword
	if bootstrap {
		input.Action, input.ResourceType, input.ResourceID = "auth.bootstrap", "administrator", p.User.ID
		method = model.SecurityAuthMethodBootstrap
	}
	audit := service.auditEvent(ctx, input)
	provenance, err := service.loginSecurity(ctx, loginSecurityInput{Principal: p, AuthMethod: method,
		AuditID: audit.ID, Now: audit.CreatedAt})
	if err != nil {
		return err
	}
	admission := store.LoginSessionAdmission{
		Session:      store.LocalSessionAdmission{User: p.User, Session: p.Session, Audit: audit, Bootstrap: bootstrap},
		Events:       provenance.Events,
		Fingerprints: provenance.Fingerprints,
	}
	return service.commitLoginSecurity(ctx, admission, provenance)
}
