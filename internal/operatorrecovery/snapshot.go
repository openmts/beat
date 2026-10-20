package operatorrecovery

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type Owner struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	TOTP        bool   `json:"totp_enabled"`
}

type Status struct {
	NextActions                []string `json:"next_actions"`
	State                      string   `json:"state"`
	Site                       string   `json:"site"`
	Schema                     int      `json:"schema"`
	Integrity                  bool     `json:"integrity"`
	Keys                       bool     `json:"keys_available"`
	LocalLogin                 bool     `json:"local_login"`
	PendingRestore             bool     `json:"pending_restore"`
	ActiveSessions             int      `json:"active_sessions"`
	Owners                     []Owner  `json:"owners"`
	users                      []model.AdminUser
	generation, rootGeneration int64
}

func inspectDatabase(ctx context.Context, config Config) (status Status, resultErr error) {
	temporary, err := os.MkdirTemp(filepath.Dir(config.DataDir), ".beat-recovery-inspect-")
	if err != nil {
		return status, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(temporary)) }()
	snapshot := filepath.Join(temporary, "beat.db")
	if err := copySnapshot(config.DatabasePath, snapshot); err != nil {
		return status, err
	}
	database, err := openDatabase(snapshot, false)
	if err != nil {
		return status, err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	if err := validateDatabase(ctx, database); err != nil {
		return status, err
	}
	if err := applicationsecrets.ValidateSnapshot(ctx, snapshot, filepath.Join(config.DataDir, "admin-data.key")); err != nil {
		return status, err
	}
	return readStatus(ctx, database)
}

func copySnapshot(source, destination string) error {
	if err := copyPrivate(source, destination); err != nil {
		return err
	}
	if _, err := os.Lstat(source + "-wal"); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return copyPrivate(source+"-wal", destination+"-wal")
}

func openDatabase(path string, writable bool) (*sql.DB, error) {
	mode := "ro"
	if writable {
		mode = "rw"
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return sql.Open("sqlite", uri+"?mode="+mode+"&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
}

func validateDatabase(ctx context.Context, database *sql.DB) error {
	if err := store.ValidateSnapshotSchema(ctx, database); err != nil {
		return err
	}
	var maximum, count int
	if err := database.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0),COUNT(*) FROM schema_migrations").Scan(&maximum, &count); err != nil {
		return err
	}
	if maximum != store.MaximumSupportedSchemaVersion || count != maximum {
		return errors.New("recovery requires the current complete schema; use the matching release or upgrade first")
	}
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return errors.New("database integrity check failed")
	}
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	resultErr := errors.Join(rows.Err(), rows.Close())
	if invalid {
		return errors.Join(resultErr, errors.New("database foreign key check failed"))
	}
	return resultErr
}

func readStatus(ctx context.Context, database *sql.DB) (Status, error) {
	status := Status{State: "ready", Schema: store.MaximumSupportedSchemaVersion, Integrity: true, Keys: true, LocalLogin: true, Owners: []Owner{}, NextActions: []string{"unlock-owner", "stage-restore"}}
	site, err := store.NewSiteSettingsStore(database).Get(ctx)
	if err != nil {
		return status, err
	}
	status.Site = site.SiteTitle
	status.users, err = store.NewAdminStore(database).ListUsers(ctx)
	if err != nil {
		return status, err
	}
	for _, user := range status.users {
		if user.Role == model.AdminRoleOwner {
			status.Owners = append(status.Owners, Owner{user.Username, user.DisplayName, user.Enabled, user.TOTPEnabled()})
		}
	}
	if len(status.Owners) == 0 {
		status.NextActions = []string{"stage-restore"}
	}
	now := time.Now().UTC()
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_sessions WHERE revoked_at IS NULL AND idle_expires_at>? AND absolute_expires_at>?", now, now).Scan(&status.ActiveSessions); err != nil {
		return status, err
	}
	err = database.QueryRowContext(ctx, "SELECT generation,root_generation FROM application_secret_state WHERE id=1").Scan(&status.generation, &status.rootGeneration)
	return status, err
}
