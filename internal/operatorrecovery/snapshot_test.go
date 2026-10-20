package operatorrecovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestSnapshotCopiesWALWithoutChangingLiveFiles(t *testing.T) {
	config := fixture(t)
	database, err := openDatabase(config.DatabasePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if _, err := database.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE site_settings SET site_title='From WAL'"); err != nil {
		t.Fatal(err)
	}
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status.Site != "From WAL" {
		t.Fatal("WAL changes lost")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copySnapshot("/absent", filepath.Join(privateTemp(t), "snapshot")); err == nil {
		t.Fatal("missing DB accepted")
	}
}

func TestStatusReadFailuresAndOwnerCollisions(t *testing.T) {
	for _, table := range []string{"site_settings", "admin_users", "admin_sessions", "application_secret_state"} {
		t.Run(table, func(t *testing.T) {
			config := fixture(t)
			database, err := openDatabase(config.DatabasePath, true)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close() }()
			if _, err := database.Exec("PRAGMA foreign_keys=OFF"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec("DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			if _, err := readStatus(t.Context(), database); err == nil {
				t.Fatal("missing table accepted")
			}
		})
	}
	session := Session{Status: Status{users: []model.AdminUser{{Username: "owner", Role: model.AdminRoleOwner}, {Username: "OWNER", Role: model.AdminRoleOwner}}}}
	if _, err := session.SelectOwner("owner"); err == nil {
		t.Fatal("normalization collision accepted")
	}
	session.Status.users = []model.AdminUser{{Username: "admin", Role: model.AdminRoleAdmin}}
	if _, err := session.SelectOwner("admin"); err == nil {
		t.Fatal("admin promoted to owner")
	}
	root := privateTemp(t)
	if err := os.WriteFile(filepath.Join(root, "db"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/missing", filepath.Join(root, "db-wal")); err != nil {
		t.Fatal(err)
	}
	if err := copySnapshot(filepath.Join(root, "db"), filepath.Join(privateTemp(t), "db")); err == nil {
		t.Fatal("invalid WAL accepted")
	}
}

func TestConfirmationEscapesMultilineSite(t *testing.T) {
	session := Session{Status: Status{Site: "First\nSecond"}}
	if got := session.Confirmation("owner"); got != `UNLOCK "First\nSecond" / owner` {
		t.Fatalf("unsafe or ambiguous confirmation: %q", got)
	}
}

func TestExistingInstanceWithoutOwnerDirectsToRestore(t *testing.T) {
	config := fixture(t)
	database, err := openDatabase(config.DatabasePath, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM admin_sessions; DELETE FROM admin_users"); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if len(session.Status.Owners) != 0 || len(session.Status.NextActions) != 1 || session.Status.NextActions[0] != "stage-restore" {
		t.Fatal("no-owner instance advertised invalid next action")
	}
	if _, err := session.SelectOwner("owner"); err == nil {
		t.Fatal("created an owner")
	}
}
