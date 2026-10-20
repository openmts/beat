package applicationsecrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestNotificationBackfillEncryptsCompleteConfigAndJoinsRotation(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	channel, err := store.NewAlertChannelStore(database.DB).CreateAlertChannel(t.Context(), &model.AlertChannel{Name: "Private webhook", ChannelType: "webhook", Enabled: true,
		Config: `{"url":"https://private-token.example/hooks/secret-path?token=PRIVATE_ENDPOINT_TOKEN","headers":{"Authorization":"Bearer PRIVATE_HEADER_TOKEN"},"template":"{{rule}}: {{message}}"}`})
	if err != nil {
		t.Fatal(err)
	}
	ensureNotificationMigration(t, database)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	var ciphertext []byte
	var public, template string
	if err := database.DB.QueryRow("SELECT secret_ciphertext,public_config_json,template_text FROM alert_channel_revisions WHERE channel_id=?", channel.ID).Scan(&ciphertext, &public, &template); err != nil {
		t.Fatal(err)
	}
	plain, err := service.OpenResource(t.Context(), ciphertext, secretbox.NotificationChannelAAD(channel.ID, 1, "webhook"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(plain, []byte("PRIVATE_HEADER_TOKEN")) || bytes.Contains([]byte(public), []byte("secret-path")) || template != "{{rule.name}}: {{event.message}}" {
		t.Fatal("legacy channel not migrated privately")
	}
	clear(plain)
	if err := validateNotificationState(t.Context(), database.DB); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "beat.db"))
	if err != nil || bytes.Contains(raw, []byte("PRIVATE_HEADER_TOKEN")) {
		t.Fatal("legacy plaintext remains on disk")
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationBackfillFailurePreservesEveryLegacyChannel(t *testing.T) {
	for _, phase := range []string{"notification_before_commit", "notification_sanitized"} {
		t.Run(phase, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			if _, err := store.NewAlertChannelStore(database.DB).CreateAlertChannel(t.Context(), &model.AlertChannel{Name: "Telegram", ChannelType: "telegram", Config: `{"bot_token":"PRIVATE_TOKEN","chat_id":"123"}`, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			ensureNotificationMigration(t, database)
			injected := errors.New("injected backfill interruption")
			if _, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, fault: func(current string) error {
				if current == phase {
					return injected
				}
				return nil
			}}); !errors.Is(err, injected) {
				t.Fatal(err)
			}
			if err := validateNotificationState(t.Context(), database.DB); err == nil {
				t.Fatal("incomplete notification migration accepted")
			}
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
		})
	}
}

func TestNotificationMessageCiphertextBindsItsMessageID(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	ensureNotificationMigration(t, database)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if err := service.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		payload, err := json.Marshal(model.NotificationPayload{Subject: "Subject", Text: "PRIVATE_BODY"})
		if err != nil {
			return err
		}
		sealed, err := access.Seal(payload, secretbox.NotificationMessageAAD("message"))
		if err != nil {
			return err
		}
		_, err = database.DB.Exec(`INSERT INTO notification_messages(id,source_key,source_kind,source_id,event_kind,payload_encrypted,payload_version,routing_state,selected_channels,created_at,retain_until)
			VALUES('message','source','alert','event','alert.triggered',?,1,'not_routed',0,1,2)`, sealed)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("UPDATE notification_messages SET id='swapped'"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err == nil {
		t.Fatal("message row swap accepted")
	}
}

func ensureNotificationMigration(t *testing.T, database *store.SQLiteStore) {
	t.Helper()
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='notification_secret_state'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		if err := store.MigrateNotifications(t.Context(), database.DB); err != nil {
			t.Fatal(err)
		}
	}
}
