package applicationsecrets

import (
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func legacyNotificationFixture(t *testing.T) (*store.SQLiteStore, string) {
	t.Helper()
	database, directory := lifecycleTestDB(t)
	_, err := store.NewAlertChannelStore(database.DB).CreateAlertChannel(t.Context(), &model.AlertChannel{Name: "Webhook", ChannelType: "webhook", Enabled: true, Config: `{"url":"https://receiver.example/PRIVATE_TOKEN"}`})
	if err != nil {
		t.Fatal(err)
	}
	ensureNotificationMigration(t, database)
	return database, directory
}

func TestNotificationBackfillRollsBackEveryDurableBoundary(t *testing.T) {
	for _, statement := range []string{
		"CREATE TRIGGER fail_revision BEFORE INSERT ON alert_channel_revisions BEGIN SELECT RAISE(ABORT,'injected'); END",
		"CREATE TRIGGER fail_channel BEFORE UPDATE ON alert_channels BEGIN SELECT RAISE(IGNORE); END",
		"CREATE TRIGGER fail_health BEFORE INSERT ON notification_channel_health BEGIN SELECT RAISE(IGNORE); END",
		"CREATE TRIGGER fail_phase BEFORE UPDATE ON notification_secret_state BEGIN SELECT RAISE(IGNORE); END",
		"CREATE INDEX block_legacy_column_drop ON alert_channels(config)",
	} {
		t.Run(statement, func(t *testing.T) {
			database, directory := legacyNotificationFixture(t)
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if service != nil {
				service.Close()
			}
			if err == nil {
				t.Fatal("incomplete notification secret migration was accepted")
			}
			var rows, columns int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM alert_channels").Scan(&rows); err != nil || rows != 1 {
				t.Fatal("failed migration lost legacy channel")
			}
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('alert_channels') WHERE name='config'").Scan(&columns); err != nil || columns != 1 {
				t.Fatal("failed migration dropped legacy configuration")
			}
		})
	}
}

func TestNotificationBackfillRejectsInvalidLegacyRows(t *testing.T) {
	for _, statement := range []string{
		"UPDATE alert_channels SET config='invalid'",
		"UPDATE alert_channels SET config=printf('%20000s','oversized')",
		"UPDATE alert_channels SET id=NULL",
		"ALTER TABLE alert_channels RENAME COLUMN config TO missing_config",
	} {
		t.Run(statement, func(t *testing.T) {
			database, directory := legacyNotificationFixture(t)
			if _, err := database.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if service != nil {
				service.Close()
			}
			if err == nil {
				t.Fatal("malformed legacy credential was silently discarded")
			}
		})
	}
}

func TestNotificationSnapshotRejectsMissingSchemaOrWrongRoot(t *testing.T) {
	database, directory := legacyNotificationFixture(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	other, otherDirectory := lifecycleTestDB(t)
	otherService, err := Open(t.Context(), Config{Database: other.DB, DataDir: otherDirectory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherService.Close)
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(otherDirectory, "admin-data.key")); err == nil {
		t.Fatal("notification snapshot accepted unrelated root key")
	}
	if _, err := database.DB.Exec("DROP TABLE notification_secret_state"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err == nil {
		t.Fatal("partial notification schema bypassed secret validation")
	}
}
