package applicationsecrets

import (
	"bytes"
	"testing"
)

func TestNotificationSanitizationRequiresWritableState(t *testing.T) {
	database, directory := legacyNotificationFixture(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if _, err := database.DB.Exec("UPDATE notification_secret_state SET phase='sanitize'"); err != nil {
		t.Fatal(err)
	}
	database.DB.SetMaxOpenConns(1)
	database.DB.SetMaxIdleConns(1)
	if _, err := database.DB.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if err := service.initializeNotifications(t.Context()); err == nil {
		t.Fatal("read-only SQLite skipped required plaintext sanitization")
	}
}

func TestNotificationStateFailuresCannotMarkBackfillReady(t *testing.T) {
	database, directory := legacyNotificationFixture(t)
	if _, err := database.DB.Exec("CREATE TRIGGER fail_ready BEFORE UPDATE ON notification_secret_state WHEN NEW.phase='ready' BEGIN SELECT RAISE(IGNORE); END"); err != nil {
		t.Fatal(err)
	}
	if service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory}); err == nil {
		service.Close()
		t.Fatal("missing readiness write accepted")
	}
	if _, err := database.DB.Exec("DROP TRIGGER fail_ready"); err != nil {
		t.Fatal(err)
	}
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if _, err := database.DB.Exec("ALTER TABLE notification_secret_state RENAME COLUMN phase TO missing_phase"); err != nil {
		t.Fatal(err)
	}
	if err := service.initializeNotifications(t.Context()); err == nil {
		t.Fatal("missing backfill phase accepted")
	}
	if err := validateNotificationState(t.Context(), database.DB); err == nil {
		t.Fatal("missing phase passed snapshot validation")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.convertNotificationChannels(t.Context()); err == nil {
		t.Fatal("backfill ignored closed database")
	}
}

func TestNotificationConversionRequiresAADAndRevisionEntropy(t *testing.T) {
	database, directory := legacyNotificationFixture(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	tx, err := database.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	channel := legacyNotificationChannel{id: "", kind: "webhook", config: `{"url":"https://receiver.example"}`, enabled: true}
	if err := service.convertNotificationChannel(t.Context(), tx, channel); err == nil {
		t.Fatal("empty secret resource identity accepted")
	}
	channel.id = "entropy-case"
	service.random = bytes.NewReader(nil)
	if err := service.convertNotificationChannel(t.Context(), tx, channel); err == nil {
		t.Fatal("revision entropy failure was ignored")
	}
}
