package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (store *AdminStore) Bootstrap(ctx context.Context, user *model.AdminUser, session *model.AdminSession) error {
	if user == nil || session == nil {
		return ErrAdminSessionNotFound
	}
	audit := model.AdminAuditEvent{ID: uuid.NewString(), RequestID: uuid.NewString(), ActorID: user.ID, ActorUsername: user.Username,
		SessionPrefix: session.TokenPrefix, Action: "auth.bootstrap", ResourceType: "administrator", ResourceID: user.ID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", CreatedAt: session.CreatedAt, IPAddress: session.IPAddress, UserAgent: session.UserAgent}
	return store.AdmitLocalSession(ctx, LocalSessionAdmission{User: *user, Session: *session, Audit: audit, Bootstrap: true})
}
