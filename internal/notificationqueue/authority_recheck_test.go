package notificationqueue

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type beforeResolve func(context.Context) error

func (callback beforeResolve) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	if err := callback(ctx); err != nil {
		return nil, err
	}
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

func TestChannelCommitRechecksAuthorityAndPolicyAfterDNS(t *testing.T) {
	cases := []struct {
		name, statement string
		want            error
	}{
		{"revoked", "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP", ErrForbidden},
		{"demoted", "UPDATE admin_users SET role='admin'", ErrForbidden},
		{"expired_proof", "UPDATE admin_sessions SET reauthenticated_until=NULL", ErrRecentAuth},
		{"policy_changed", "UPDATE notification_settings SET revision=revision+1", ErrConflict},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s, db := queueFixture(t)
			actor := queueOwner(t, s)
			s.egress.Resolver = beforeResolve(func(ctx context.Context) error {
				_, err := db.DB.ExecContext(ctx, test.statement)
				return err
			})
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			write := model.NotificationChannelWrite{Name: "Webhook", ChannelType: "webhook", Enabled: true, Config: json.RawMessage(`{"endpoint":"https://receiver.example"}`)}
			if _, err := s.CreateChannel(ctx, actor, write); !errors.Is(err, test.want) {
				t.Fatalf("want %v after DNS, got %v", test.want, err)
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM alert_channels").Scan(&count); err != nil || count != 0 {
				t.Fatal("stale authority or network policy committed a channel")
			}
		})
	}
}

func TestChannelCommitRejectsConcurrentRevisionOrDeletion(t *testing.T) {
	for _, statement := range []string{"UPDATE alert_channels SET current_revision=2", "UPDATE alert_channels SET deleted_at=CURRENT_TIMESTAMP"} {
		t.Run(statement, func(t *testing.T) {
			s, db := queueFixture(t)
			actor := queueOwner(t, s)
			addQueueChannel(t, s, "channel")
			s.egress.Resolver = beforeResolve(func(ctx context.Context) error {
				_, err := db.DB.ExecContext(ctx, statement)
				return err
			})
			write := model.NotificationChannelWrite{Name: "Changed", ChannelType: "webhook", ExpectedRevision: 1, Enabled: true, Config: json.RawMessage(`{"endpoint":"https://receiver.example"}`)}
			_, err := s.UpdateChannel(t.Context(), actor, ChannelUpdate{ID: "channel", Write: write})
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
				t.Fatalf("concurrent channel change accepted: %v", err)
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM alert_channel_revisions").Scan(&count); err != nil || count != 1 {
				t.Fatal("stale revision produced a new secret revision")
			}
		})
	}
}

func TestEnqueueStorageFailureDegradesReadinessUntilDurableWrite(t *testing.T) {
	s, db := queueFixture(t)
	if _, err := db.DB.Exec("CREATE TRIGGER reject_outbox BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatal("outbox write failure was not propagated")
	}
	if err := s.Ready(t.Context()); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatal("read-only readiness hid the outbox write failure")
	}
	if _, err := db.DB.Exec("DROP TRIGGER reject_outbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(t.Context()); err != nil {
		t.Fatal("successful durable write did not restore readiness")
	}
}
