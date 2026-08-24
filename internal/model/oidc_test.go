package model

import (
	"testing"
	"time"
)

func TestClientSecretConfigured(t *testing.T) {
	provider := OIDCProvider{}
	if provider.ClientSecretConfigured() {
		t.Fatal("empty secret must report unconfigured")
	}
	provider.ClientSecretEncrypted = []byte{1, 2, 3}
	if !provider.ClientSecretConfigured() {
		t.Fatal("non-empty secret must report configured")
	}
}

func TestOIDCTransactionExpired(t *testing.T) {
	now := time.Now().UTC()
	transaction := OIDCTransaction{ExpiresAt: now.Add(-time.Minute)}
	if !transaction.Expired(now) {
		t.Fatal("past-expiry transaction must be expired")
	}
	transaction.ExpiresAt = now.Add(time.Minute)
	if transaction.Expired(now) {
		t.Fatal("future-expiry transaction must not be expired")
	}
}

func TestOIDCProviderPublicFields(t *testing.T) {
	public := OIDCProviderPublic{ID: "p", Slug: "slug", DisplayName: "Name"}
	if public.ID != "p" || public.Slug != "slug" || public.DisplayName != "Name" {
		t.Fatalf("public provider = %#v", public)
	}
}

func TestAuthMethodConstants(t *testing.T) {
	if AuthMethodLocal != "local" || AuthMethodOIDC != "oidc" {
		t.Fatalf("auth method constants = %q / %q", AuthMethodLocal, AuthMethodOIDC)
	}
	if OIDCPurposeLogin != "login" || OIDCPurposeLink != "link" {
		t.Fatalf("oidc purpose constants = %q / %q", OIDCPurposeLogin, OIDCPurposeLink)
	}
}
