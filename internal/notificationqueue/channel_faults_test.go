package notificationqueue

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func faultChannelWrite() model.NotificationChannelWrite {
	return model.NotificationChannelWrite{Name: "Changed", ChannelType: "webhook", Enabled: true, ExpectedRevision: 1, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8/new"}`)}
}

func TestChannelWritesRollbackAllRevisionAndAuditBoundaries(t *testing.T) {
	for _, statement := range []string{
		"CREATE TRIGGER reject_channel BEFORE INSERT ON alert_channels BEGIN SELECT RAISE(IGNORE); END",
		"CREATE TRIGGER reject_revision BEFORE INSERT ON alert_channel_revisions BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER reject_health BEFORE INSERT ON notification_channel_health BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER corrupt_view AFTER INSERT ON alert_channel_revisions BEGIN UPDATE alert_channel_revisions SET secret_present_json='invalid' WHERE id=NEW.id; END",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			fixture.sql(t, statement)
			if _, err := fixture.service.CreateChannel(t.Context(), fixture.actor, faultChannelWrite()); err == nil {
				t.Fatal("channel boundary failure was ignored")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM alert_channels", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM alert_channel_revisions", 1)
		})
	}
}

func TestChannelWritesFailClosedOnSecretAndEntropyFailures(t *testing.T) {
	cases := []struct {
		name  string
		setup func(queueFaultFixture)
	}{
		{"seal", func(f queueFaultFixture) {
			f.service.secrets = failingResourceService{base: f.service.secrets, seal: true}
		}},
		{"open", func(f queueFaultFixture) {
			f.service.secrets = failingResourceService{base: f.service.secrets, open: true}
		}},
		{"write_window", func(f queueFaultFixture) {
			f.service.secrets = failingResourceService{base: f.service.secrets, write: true}
		}},
		{"revision_entropy", func(f queueFaultFixture) { f.service.random = bytes.NewReader(nil) }},
		{"audit_entropy", func(f queueFaultFixture) { f.service.random = bytes.NewReader(bytes.Repeat([]byte{8}, 16)) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			test.setup(fixture)
			_, err := fixture.service.UpdateChannel(t.Context(), fixture.actor, ChannelUpdate{ID: "channel", Write: faultChannelWrite()})
			if err == nil {
				t.Fatal("secret or entropy failure was ignored")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM alert_channel_revisions", 1)
			fixture.assertCount(t, "SELECT COUNT(*) FROM admin_audit_events", 0)
		})
	}
}

func TestChannelTypeChangeRequiresNewReviewedSecrets(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	write := faultChannelWrite()
	write.ChannelType, write.Config = "custom_http", json.RawMessage(`{}`)
	if _, err := fixture.service.UpdateChannel(t.Context(), fixture.actor, ChannelUpdate{ID: "channel", Write: write}); err == nil {
		t.Fatal("provider type change reused old provider secrets")
	}
	write.Config = json.RawMessage(`{"endpoint":"https://8.8.8.8/new","body":{"text":"{{text}}"}}`)
	if _, err := fixture.service.UpdateChannel(t.Context(), fixture.actor, ChannelUpdate{ID: "channel", Write: write}); err != nil {
		t.Fatal(err)
	}
	write.ExpectedRevision = 0
	if _, err := fixture.service.UpdateChannel(t.Context(), fixture.actor, ChannelUpdate{ID: "channel", Write: write}); !errors.Is(err, ErrConflict) {
		t.Fatal("missing expected revision accepted")
	}
	write.ExpectedRevision = 1
	if _, err := fixture.service.UpdateChannel(t.Context(), fixture.actor, ChannelUpdate{ID: "missing", Write: write}); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing channel accepted")
	}
}

func TestChannelValidationCoversNamesEventsAndSubjects(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	for _, write := range []model.NotificationChannelWrite{
		{Name: "bad\nname"}, {Name: "Channel", EventKinds: make([]string, 100)},
		{Name: "Channel", EventKinds: []string{"notification.test"}},
		{Name: "Channel", TemplateSubject: "{{env.SECRET}}"},
	} {
		if _, err := fixture.service.CreateChannel(t.Context(), fixture.actor, write); err == nil {
			t.Fatal("invalid channel metadata accepted")
		}
	}
}
