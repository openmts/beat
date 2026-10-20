package backup

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func notificationBackupQueue(t *testing.T, fixture *backupFixture) *notificationqueue.Service {
	t.Helper()
	secrets, ok := fixture.service.secretLifecycle.(secretbox.ResourceService)
	if !ok {
		t.Fatal("backup fixture has no application secret coordinator")
	}
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: fixture.sqlite.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	now, recent := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
	admins := store.NewAdminStore(fixture.sqlite.DB)
	if err := admins.CreateUser(t.Context(), &model.AdminUser{ID: "notification-owner", Username: "notification-owner", DisplayName: "Owner", Role: "owner", Enabled: true, PasswordHash: "fixture", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: "notification-session", UserID: "notification-owner", TokenHash: bytes.Repeat([]byte{3}, 32), TokenPrefix: "fixture", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: recent, AbsoluteExpiresAt: recent, ReauthenticatedUntil: &recent}); err != nil {
		t.Fatal(err)
	}
	actor := notificationqueue.Actor{UserID: "notification-owner", SessionID: "notification-session"}
	write := model.NotificationChannelWrite{Name: "Backup webhook", ChannelType: "webhook", Enabled: true, EventKinds: []string{"alert.triggered"}, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8/PRIVATE_ENDPOINT","headers":{"Authorization":"PRIVATE_AUTH"}}`)}
	if _, err := queue.CreateChannel(t.Context(), actor, write); err != nil {
		t.Fatal(err)
	}
	return queue
}

func seedNotificationBackup(t *testing.T, fixture *backupFixture) {
	t.Helper()
	queue := notificationBackupQueue(t, fixture)
	for _, key := range []string{"expired-lease", "terminal-success"} {
		request := notificationqueue.Request{SourceKey: key, SourceKind: "alert", SourceID: key, EventKind: "alert.triggered", Subject: "Subject", Text: "PRIVATE_NOTIFICATION_BODY", UseSubscriptions: true}
		receipt, err := queue.Enqueue(t.Context(), request, nil)
		if err != nil {
			t.Fatal(err)
		}
		id := receipt.Deliveries[0].ID
		if key == "terminal-success" {
			if _, err := fixture.sqlite.DB.Exec("UPDATE notification_deliveries SET state='succeeded',completed_at=? WHERE id=?", time.Now().UnixMilli(), id); err != nil {
				t.Fatal(err)
			}
			continue
		}
		expired := time.Now().Add(-time.Minute).UnixMilli()
		if _, err := fixture.sqlite.DB.Exec("UPDATE notification_deliveries SET state='leased',attempt_count=1,lease_token_hash=?,lease_expires_at=?,worker_id='old-worker' WHERE id=?", bytes.Repeat([]byte{4}, 32), expired, id); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.sqlite.DB.Exec("INSERT INTO notification_delivery_attempts(delivery_id,attempt_number,started_at,outcome) VALUES(?,1,?,'in_progress')", id, expired); err != nil {
			t.Fatal(err)
		}
	}
}

func restoreNotificationArchive(t *testing.T, fixture *backupFixture, record model.BackupRecord) string {
	t.Helper()
	target := t.TempDir()
	backups := filepath.Join(target, "backups")
	if err := os.Mkdir(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(backups, record.Filename)
	if err := copyFile(filepath.Join(fixture.dataDir, "backups", record.Filename), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(filepath.Join(target, "restore.pending.json"), restoreJournal(record.ID, archive)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPending(t.Context(), target, filepath.Join(target, "beat.db"), filepath.Join(target, "beat_mts")); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestNotificationBackupRestoresEncryptedQueueWithoutSending(t *testing.T) {
	fixture := newBackupFixture(t)
	seedNotificationBackup(t, fixture)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	archive, _, err := fixture.service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertNotificationBackupFormat(t, archive)
	uploaded, err := fixture.service.ValidateUpload(t.Context(), archive)
	closeErr := archive.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("notification archive validation: %v %v", err, closeErr)
	}
	target := restoreNotificationArchive(t, fixture, uploaded)
	restored, err := store.NewSQLiteStore(filepath.Join(target, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	secrets := testutil.OpenSecrets(t, restored.DB, target)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: restored.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	for source, want := range map[string]string{"expired-lease": "pending", "terminal-success": "succeeded"} {
		receipt, err := queue.SourceReceipt(t.Context(), source)
		if err != nil || receipt.State != want {
			t.Fatalf("restored source %s: expected %s, got %s (%v)", source, want, receipt.State, err)
		}
	}
	var integrity string
	if err := restored.DB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("restored notification database failed integrity check")
	}
	var audits int
	if err := restored.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='notification.restore.reconcile'").Scan(&audits); err != nil || audits != 1 {
		t.Fatal("restore reconciliation audit is missing")
	}
	content, err := os.ReadFile(filepath.Join(target, "beat.db"))
	if err != nil || bytes.Contains(content, []byte("PRIVATE_NOTIFICATION_BODY")) || bytes.Contains(content, []byte("PRIVATE_AUTH")) {
		t.Fatal("restore materialized notification plaintext")
	}
}

func assertNotificationBackupFormat(t *testing.T, file *os.File) {
	t.Helper()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := archive.Open(manifestEntry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manifest.Close() }()
	var payload model.BackupManifest
	if err := json.NewDecoder(manifest).Decode(&payload); err != nil || payload.FormatVersion != 4 {
		t.Fatal("notifications changed the reviewed backup format")
	}
}
