package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const (
	servicePrincipalResourceType    = "service_token"
	servicePrincipalActionCreate    = "service_token.create"
	servicePrincipalActionRotate    = "service_token.rotate"
	servicePrincipalActionRevoke    = "service_token.revoke"
	servicePrincipalActionRevokeAll = "service_token.revoke_all"
	servicePrincipalActionExpire    = "service_token.expire"
)

// servicePrincipalAuditDetail 只保留有界的非机密元数据，摘要与明文都不进入审计。
type servicePrincipalAuditDetail struct {
	Name        string    `json:"name"`
	Prefix      string    `json:"prefix"`
	Visibility  string    `json:"visibility"`
	Scopes      []string  `json:"scopes"`
	Resources   int       `json:"resources"`
	SourceCIDRs int       `json:"source_cidrs"`
	ExpiresAt   time.Time `json:"expires_at"`
	Reason      string    `json:"reason,omitempty"`
	RotatedFrom string    `json:"rotated_from,omitempty"`
	Revoked     int       `json:"revoked,omitempty"`
}

func servicePrincipalAudit(ctx context.Context, database schemaExecutor, action string, resourceID string,
	actor ServicePrincipalActor, detail servicePrincipalAuditDetail) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	event := model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: actor.RequestID, ActorID: actor.UserID,
		ActorUsername: servicePrincipalActorName(actor), Action: action, ResourceType: servicePrincipalResourceType,
		ResourceID: resourceID, Outcome: model.AuditOutcomeSuccess, DetailJSON: string(encoded),
		IPAddress: actor.IPAddress, UserAgent: actor.UserAgent, SessionPrefix: servicePrincipalSessionPrefix(actor.SessionID),
		CreatedAt: model.NowUTC(),
	}
	return insertAuditEvent(ctx, database, &event)
}

func servicePrincipalActorName(actor ServicePrincipalActor) string {
	if actor.UserID == "" {
		return "system"
	}
	return actor.UserID
}

func servicePrincipalSessionPrefix(sessionID string) string {
	const prefixLength = 8
	if len(sessionID) <= prefixLength {
		return sessionID
	}
	return sessionID[:prefixLength]
}

func servicePrincipalDetail(value *model.ServicePrincipal) servicePrincipalAuditDetail {
	scopes := make([]string, 0, len(value.Scopes))
	for _, scope := range value.Scopes {
		scopes = append(scopes, string(scope))
	}
	return servicePrincipalAuditDetail{
		Name: value.Name, Prefix: value.TokenPrefix, Visibility: string(value.Visibility), Scopes: scopes,
		Resources: len(value.Resources), SourceCIDRs: len(value.SourceCIDRs), ExpiresAt: value.ExpiresAt.UTC(),
	}
}
