package oidc

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type failingLoginSecrets struct{ secretbox.ResourceService }

func (secrets failingLoginSecrets) WithWrite(ctx context.Context, action func(secretbox.ResourceAccess) error) error {
	return secrets.ResourceService.WithWrite(ctx, func(access secretbox.ResourceAccess) error { return action(failingLoginAccess{access}) })
}

func (secrets failingLoginSecrets) OpenResource(context.Context, []byte, secretbox.ResourceAAD) ([]byte, error) {
	return nil, errors.New("unavailable")
}

type failingLoginAccess struct{ secretbox.ResourceAccess }

func (access failingLoginAccess) Seal([]byte, secretbox.ResourceAAD) ([]byte, error) {
	return nil, errors.New("unavailable")
}

func (access failingLoginAccess) Open([]byte, secretbox.ResourceAAD) ([]byte, error) {
	return nil, errors.New("unavailable")
}

func TestOIDCCredentialPersistenceErrorsDoNotContinue(t *testing.T) {
	service, _, database, secrets, idp, _ := setupOIDCService(t)
	provider := createTestProvider(t, service, idp.baseURL(), true)
	service.secrets = failingLoginSecrets{secrets}
	if err := service.persistTransaction(t.Context(), &model.OIDCTransaction{ID: "failure", ProviderID: provider.ID}, "verifier"); err == nil {
		t.Fatal("verifier encryption failure ignored")
	}
	if err := service.persistNewProvider(t.Context(), &model.OIDCProvider{ID: "failure", ConfigurationVersion: 1}, "secret"); err == nil {
		t.Fatal("provider encryption failure ignored")
	}
	if _, err := service.providerSecret(provider); err == nil {
		t.Fatal("provider decryption failure ignored")
	}
	if err := service.persistUpdatedProvider(t.Context(), provider, ""); err == nil {
		t.Fatal("provider open failure ignored")
	}
	provider.ConfigurationVersion++
	if err := service.persistUpdatedProvider(t.Context(), provider, "replacement"); err == nil {
		t.Fatal("replacement seal failure ignored")
	}
	service.secrets = secrets
	if err := service.persistUpdatedProvider(t.Context(), &model.OIDCProvider{ID: "missing"}, ""); err == nil {
		t.Fatal("missing provider updated")
	}
	if _, err := database.DB.Exec("ALTER TABLE admin_oidc_providers RENAME TO unavailable_providers"); err != nil {
		t.Fatal(err)
	}
	if err := service.persistUpdatedProvider(t.Context(), provider, ""); err == nil {
		t.Fatal("failed provider read ignored")
	}
}

func TestOIDCBeginAndCallbackFailClosed(t *testing.T) {
	service, identities, database, secrets, idp, user := setupOIDCService(t)
	provider := createTestProvider(t, service, idp.baseURL(), true)
	for _, size := range []int{0, oauthStateBytes, oauthStateBytes + nonceBytes, oauthStateBytes + nonceBytes + verifierBytes} {
		service.random = bytes.NewReader(make([]byte, size))
		if _, err := service.BeginLogin(t.Context(), provider.ID, "/admin", "https://example.invalid", "", " "); err == nil {
			t.Fatal("entropy failure ignored", size)
		}
	}
	until := time.Now().Add(time.Hour)
	principal := &model.AdminPrincipal{User: *user, Session: model.AdminSession{ReauthenticatedUntil: &until}}
	if _, err := service.BeginLink(t.Context(), principal, "missing", "/admin", "https://example.invalid", "", ""); err == nil {
		t.Fatal("missing link provider accepted")
	}
	if _, err := service.BeginLink(t.Context(), principal, provider.ID, "/outside", "https://example.invalid", "", ""); err == nil {
		t.Fatal("unsafe link return accepted")
	}
	if _, err := service.BeginLogin(t.Context(), provider.ID, "/outside", "https://example.invalid", "", ""); err == nil {
		t.Fatal("unsafe login return accepted")
	}
	state, binding := createLoginTransaction(t, secrets, identities, provider)
	service.secrets = failingLoginSecrets{secrets}
	if _, token, err := service.HandleCallback(t.Context(), state, "code", binding, "", ""); err == nil || token != "" {
		t.Fatal("failed verifier decryption admitted")
	}
	if _, err := database.DB.Exec("ALTER TABLE admin_oidc_transactions RENAME TO unavailable_transactions"); err != nil {
		t.Fatal(err)
	}
	if _, token, err := service.HandleCallback(t.Context(), state, "code", binding, "", ""); err == nil || token != "" {
		t.Fatal("failed transaction read admitted")
	}
}

func TestOIDCLinkCannotAdmitMissingDisabledOrUnavailableUser(t *testing.T) {
	service, _, database, _, idp, user := setupOIDCService(t)
	provider := createTestProvider(t, service, idp.baseURL(), true)
	for _, id := range []string{"missing", user.ID} {
		if id == user.ID {
			if _, err := database.DB.Exec("UPDATE admin_users SET enabled=0"); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := service.completeLink(t.Context(), provider, &model.OIDCTransaction{UserID: id}, IdentityClaims{Subject: "subject"}, "", ""); err == nil {
			t.Fatal("ineligible user linked")
		}
	}
	for _, scopes := range [][]string{nil, {"profile"}, {"openid", "bad scope"}} {
		if err := validateScopes(scopes); err == nil {
			t.Fatal("invalid scopes accepted")
		}
	}
}
