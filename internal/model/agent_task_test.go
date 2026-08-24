package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAuthorizedKeyPayloadRoundTrip(t *testing.T) {
	payload, err := EncodeAuthorizedKeyPayload("ssh-ed25519 AAAA beat")
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	decoded, err := DecodeAuthorizedKeyPayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.PublicKey != "ssh-ed25519 AAAA beat" {
		t.Fatalf("public key = %q", decoded.PublicKey)
	}
}

func TestDecodeAuthorizedKeyPayloadRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "malformed json", payload: "{"},
		{name: "missing public key", payload: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeAuthorizedKeyPayload(test.payload)
			if err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestCustomMetricRegisterPayloadRoundTrip(t *testing.T) {
	metrics := []CustomMetric{{ID: "metric-1", Name: "custom_latency", ShellCommand: "echo 1", Enabled: true}}
	payload, err := EncodeCustomMetricRegisterPayload(metrics)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	decoded, err := DecodeCustomMetricRegisterPayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(decoded.Metrics) != 1 || decoded.Metrics[0].Name != "custom_latency" {
		t.Fatalf("decoded metrics = %#v", decoded.Metrics)
	}
}

func TestDecodeCustomMetricRegisterPayloadRejectsInvalidPayload(t *testing.T) {
	if _, err := DecodeCustomMetricRegisterPayload("{"); err == nil {
		t.Fatal("expected malformed JSON error")
	}
	_, err := DecodeCustomMetricRegisterPayload(`{}`)
	if !errors.Is(err, ErrAgentTaskInvalidPayload) {
		t.Fatalf("decode error = %v, want ErrAgentTaskInvalidPayload", err)
	}
}

func TestAdminSessionActiveAndRecentAuthentication(t *testing.T) {
	now := time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC)
	reauthUntil := now.Add(time.Minute)
	session := AdminSession{
		IdleExpiresAt:        now.Add(time.Minute),
		AbsoluteExpiresAt:    now.Add(time.Hour),
		ReauthenticatedUntil: &reauthUntil,
	}
	if !session.Active(now) {
		t.Fatal("expected active session")
	}
	if !session.RecentlyAuthenticated(now) {
		t.Fatal("expected recent authentication")
	}

	revokedAt := now.Add(-time.Second)
	session.RevokedAt = &revokedAt
	if session.Active(now) {
		t.Fatal("expected revoked session to be inactive")
	}
	if session.RecentlyAuthenticated(reauthUntil) {
		t.Fatal("expected authentication window to expire at boundary")
	}
}

func TestAdminUserTOTPEnabledAndStringRedactsSecrets(t *testing.T) {
	enabledAt := time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC)
	user := AdminUser{
		ID: "user-1", Username: "owner", DisplayName: "Owner",
		Role: AdminRoleOwner, PasswordHash: "hash",
		TOTPSecretEncrypted: []byte("secret"), TOTPEnabledAt: &enabledAt,
	}
	if !user.TOTPEnabled() {
		t.Fatal("expected TOTP to be enabled")
	}
	serialized := user.String()
	if strings.Contains(serialized, "hash") || strings.Contains(serialized, "secret") {
		t.Fatalf("serialized user leaks secret fields: %s", serialized)
	}
	if !strings.Contains(serialized, `"username":"owner"`) {
		t.Fatalf("serialized user = %s", serialized)
	}

	user.TOTPSecretEncrypted = nil
	if user.TOTPEnabled() {
		t.Fatal("expected missing encrypted secret to disable TOTP")
	}
}
