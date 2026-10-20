package adminauth

import (
	"database/sql"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func oidcSessionTestRequest(t *testing.T, service *Service, userID, identityID string) OIDCSessionRequest {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	target := store.NewOIDCStore(db)
	now := service.now()
	provider := model.OIDCProvider{ID: "provider", Slug: "provider", DisplayName: "Provider", IssuerURL: "https://oidc.invalid", ClientID: "client", Enabled: true, ConfigurationVersion: 1, Scopes: []string{"openid"}, CreatedAt: now, UpdatedAt: now}
	identity := model.ExternalIdentity{ID: identityID, ProviderID: provider.ID, UserID: userID, Subject: "subject", CreatedAt: now, UpdatedAt: now, LastLoginAt: now}
	if err := target.CreateProvider(t.Context(), &provider); err != nil {
		t.Fatal(err)
	}
	if err := target.LinkIdentity(t.Context(), &identity); err != nil {
		t.Fatal(err)
	}
	return OIDCSessionRequest{Provider: provider, Identity: identity, IPAddress: "127.0.0.1", UserAgent: "agent"}
}
