package notificationqueue

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func queueOwner(t *testing.T, s *Service) Actor {
	t.Helper()
	now := s.now()
	recent := now.Add(time.Minute)
	admins := store.NewAdminStore(s.db)
	if err := admins.CreateUser(t.Context(), &model.AdminUser{ID: "owner", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true,
		PasswordHash: "fixture", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: "session", UserID: "owner", TokenHash: bytes.Repeat([]byte{4}, 32), TokenPrefix: "prefix", CreatedAt: now,
		LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), ReauthenticatedUntil: &recent}); err != nil {
		t.Fatal(err)
	}
	return Actor{UserID: "owner", SessionID: "session", RequestID: "request"}
}

func TestChannelRevisionRetainsBlankSecretsAndNeverReturnsThem(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	write := model.NotificationChannelWrite{Name: "Webhook", ChannelType: "webhook", Enabled: true, EventKinds: []string{"alert.triggered"}, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8/PRIVATE_PATH?token=PRIVATE_TOKEN","headers":{"Authorization":"Bearer PRIVATE_AUTH"}}`)}
	channel, err := s.CreateChannel(t.Context(), actor, write)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(channel)
	if err != nil || strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("channel returned secret")
	}
	write.ExpectedRevision = 1
	write.Config = json.RawMessage(`{"endpoint":""}`)
	updated, err := s.UpdateChannel(t.Context(), actor, ChannelUpdate{ID: channel.ID, Write: write})
	if err != nil || updated.CurrentRevision != 2 || !updated.SecretPresent["endpoint"] {
		t.Fatal(err)
	}
	if _, err := s.UpdateChannel(t.Context(), actor, ChannelUpdate{ID: channel.ID, Write: write}); err != ErrConflict {
		t.Fatal("stale revision accepted")
	}
	var revisions int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM alert_channel_revisions WHERE channel_id=?", channel.ID).Scan(&revisions); err != nil || revisions != 2 {
		t.Fatal("immutable revision lost")
	}
	channels, err := s.ListChannels(t.Context())
	if err != nil || len(channels) != 1 {
		t.Fatal(err)
	}
}

func TestChannelMutationAuditAndAuthorityFailuresRollback(t *testing.T) {
	s, db := queueFixture(t)
	actor := queueOwner(t, s)
	write := model.NotificationChannelWrite{Name: "Webhook", ChannelType: "webhook", Enabled: true, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8"}`)}
	if _, err := db.DB.Exec("CREATE TRIGGER deny_notification_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel(t.Context(), actor, write); err == nil {
		t.Fatal("audit failure accepted")
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM alert_channels").Scan(&count); err != nil || count != 0 {
		t.Fatal("audit failure committed channel")
	}
	if _, err := db.DB.Exec("DROP TRIGGER deny_notification_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE admin_users SET role='admin'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel(t.Context(), actor, write); err != ErrForbidden {
		t.Fatal("demoted owner created channel")
	}
}
