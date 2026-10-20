package store

import (
	"bytes"
	"context"
	"database/sql"
	"slices"

	"github.com/beat/backend/internal/model"
)

func checkOIDCAdmissionAuthority(ctx context.Context, tx *sql.Tx, a OIDCSessionAdmission) error {
	if err := checkLoginUser(ctx, tx, a.User); err != nil {
		return err
	}
	provider, err := scanOIDCProvider(tx.QueryRowContext(ctx, "SELECT "+oidcProviderColumns+" FROM admin_oidc_providers WHERE id=?", a.Provider.ID))
	if err != nil {
		return err
	}
	if provider == nil || !provider.Enabled || provider.ConfigurationVersion != a.Provider.ConfigurationVersion ||
		provider.IssuerURL != a.Provider.IssuerURL || provider.ClientID != a.Provider.ClientID || !slices.Equal(provider.Scopes, a.Provider.Scopes) ||
		!provider.CreatedAt.Equal(a.Provider.CreatedAt) {
		return ErrAdminSessionNotFound
	}
	identity, err := scanExternalIdentity(tx.QueryRowContext(ctx, "SELECT "+externalIdentityColumns+" FROM admin_external_identities WHERE id=?", a.Identity.ID))
	if err != nil {
		return err
	}
	if identity == nil || identity.ProviderID != a.Provider.ID || identity.UserID != a.User.ID || identity.Subject != a.Identity.Subject ||
		!identity.CreatedAt.Equal(a.Identity.CreatedAt) {
		return ErrAdminSessionNotFound
	}
	return nil
}

func checkOIDCAdmissionResult(ctx context.Context, tx *sql.Tx, a OIDCSessionAdmission) error {
	if err := checkOIDCAdmissionAuthority(ctx, tx, a); err != nil {
		return err
	}
	session, err := scanAdminSession(tx.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id=?", a.Session.ID))
	if err != nil {
		return err
	}
	if !sameOIDCSession(session, a.Session) {
		return ErrAdminSessionNotFound
	}
	if err := checkOIDCLoginTimes(ctx, tx, a); err != nil {
		return err
	}
	var count int
	e := a.Audit
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE id=? AND request_id=? AND actor_id=? AND actor_username=? AND action=? AND resource_type=? AND resource_id=? AND outcome=? AND session_prefix=? AND detail_json=? AND ip_address=? AND user_agent=? AND created_at=?`, e.ID, e.RequestID, e.ActorID, e.ActorUsername, e.Action, e.ResourceType, e.ResourceID, e.Outcome, e.SessionPrefix, e.DetailJSON, e.IPAddress, e.UserAgent, e.CreatedAt).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrAdminSessionNotFound
	}
	return nil
}

func sameOIDCSession(actual *model.AdminSession, expected model.AdminSession) bool {
	return actual != nil && actual.UserID == expected.UserID && bytes.Equal(actual.TokenHash, expected.TokenHash) &&
		actual.TokenPrefix == expected.TokenPrefix && actual.RevokedAt == nil && actual.ReauthenticatedUntil == nil &&
		actual.AuthenticationMethod == model.AuthMethodOIDC && actual.ExternalIdentityID != nil && expected.ExternalIdentityID != nil && *actual.ExternalIdentityID == *expected.ExternalIdentityID &&
		actual.CreatedAt.Equal(expected.CreatedAt) && actual.LastActivityAt.Equal(expected.LastActivityAt) &&
		actual.IdleExpiresAt.Equal(expected.IdleExpiresAt) && actual.AbsoluteExpiresAt.Equal(expected.AbsoluteExpiresAt) &&
		actual.IPAddress == expected.IPAddress && actual.UserAgent == expected.UserAgent
}
