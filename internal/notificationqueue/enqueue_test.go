package notificationqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func queueFixture(t *testing.T) (*Service, *store.SQLiteStore) {
	t.Helper()
	db, err := store.NewSQLiteStore(queueFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='notification_secret_state'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		if err := store.MigrateNotifications(t.Context(), db.DB); err != nil {
			t.Fatal(err)
		}
	}
	secrets := testutil.NewSecrets(t, db.DB)
	service, err := NewService(Config{Database: db.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, db
}

func addQueueChannel(t *testing.T, s *Service, id string) {
	t.Helper()
	canonical, err := notification.CanonicalizeProvider(notification.ConfigWrite{ChannelType: "webhook", Config: json.RawMessage(`{"endpoint":"https://receiver.example/path?token=PRIVATE"}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	err = s.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		ciphertext, err := access.Seal(canonical.Secret, secretbox.NotificationChannelAAD(id, 1, "webhook"))
		if err != nil {
			return err
		}
		present, err := json.Marshal(canonical.Present)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(`INSERT INTO alert_channels(id,name,channel_type,enabled,current_revision,event_kinds_json,created_at,updated_at)
			VALUES(?,?,'webhook',1,1,'["alert.triggered"]',?,?)`, id, id, time.Now(), time.Now()); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO alert_channel_revisions(id,channel_id,revision,channel_type,public_config_json,secret_ciphertext,secret_present_json,created_by,created_at,change_reason)
			VALUES(?,?,1,'webhook',?,?,?,'fixture',1,'fixture')`, id+"-revision", id, string(canonical.Public), ciphertext, string(present)); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO notification_channel_health(channel_id,current_revision,updated_at) VALUES(?,1,1)", id); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func enqueueRequest(key string) Request {
	return Request{SourceKey: key, SourceKind: "alert", SourceID: key, EventKind: "alert.triggered", Subject: "Subject", Text: "PRIVATE_TEXT", Data: map[string]string{"event.message": "PRIVATE_DATA"}, UseSubscriptions: true}
}

func TestEnqueueCommitsBusinessMutationWithEncryptedFanout(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "first")
	addQueueChannel(t, s, "second")
	if _, err := db.DB.Exec("CREATE TABLE source_state(id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	mutation := func(tx *sql.Tx) error { _, err := tx.Exec("INSERT INTO source_state VALUES('event')"); return err }
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), mutation)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "pending" || len(receipt.Deliveries) != 2 || receipt.Counts.Pending != 2 {
		t.Fatalf("fanout = %#v", receipt)
	}
	duplicate, err := s.Enqueue(t.Context(), enqueueRequest("event"), mutation)
	if err != nil || !duplicate.Duplicate || duplicate.MessageID != receipt.MessageID {
		t.Fatal("producer retry was not idempotent")
	}
	var payload []byte
	if err := db.DB.QueryRow("SELECT payload_encrypted FROM notification_messages").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !secretbox.IsV2Envelope(payload) {
		t.Fatal("message stored without envelope")
	}
	plain, err := s.secrets.OpenResource(t.Context(), payload, secretbox.NotificationMessageAAD(receipt.MessageID))
	if err != nil {
		t.Fatal(err)
	}
	var message model.NotificationPayload
	if err := json.Unmarshal(plain, &message); err != nil || message.Text != "PRIVATE_TEXT" {
		t.Fatal("payload lost")
	}
}

func TestEnqueueRollsBackMutationOnOutboxFailureAndSkip(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{true: "skip", false: "write_failure"}[skip], func(t *testing.T) {
			s, db := queueFixture(t)
			if _, err := db.DB.Exec("CREATE TABLE source_state(id TEXT PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.Exec("CREATE TRIGGER reject_message BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
				t.Fatal(err)
			}
			receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), func(tx *sql.Tx) error {
				if _, err := tx.Exec("INSERT INTO source_state VALUES('event')"); err != nil {
					return err
				}
				if skip {
					return ErrSkipMutation
				}
				return nil
			})
			if skip {
				if err != nil || !receipt.Skipped {
					t.Fatal("controlled skip was not recognized")
				}
			} else if err == nil {
				t.Fatal("outbox failure accepted")
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM source_state").Scan(&count); err != nil || count != 0 {
				t.Fatal("business mutation escaped rollback")
			}
		})
	}
}

func TestUnroutedMessagesAndCurrentRetryLeafReceipt(t *testing.T) {
	s, db := queueFixture(t)
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("unrouted"), nil)
	if err != nil || receipt.State != "not_routed" {
		t.Fatal(err)
	}
	addQueueChannel(t, s, "channel")
	receipt, err = s.Enqueue(t.Context(), enqueueRequest("routed"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET state='dead' WHERE id=?", receipt.Deliveries[0].ID); err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.insertDelivery(t.Context(), tx, newDelivery{messageID: receipt.MessageID, channel: selectedChannel{id: "channel", revision: 1}, retryOf: receipt.Deliveries[0].ID})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE notification_deliveries SET state='succeeded' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	updated, err := s.SourceReceipt(t.Context(), "routed")
	if err != nil || updated.State != "succeeded" || len(updated.Deliveries) != 1 || updated.Deliveries[0].ID != id {
		t.Fatal("retry leaf not reflected")
	}
	if _, err := s.SourceReceipt(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
