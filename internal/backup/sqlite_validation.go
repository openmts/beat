package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/beat/backend/internal/store"
)

func openArchivedSQLite(path string) (*sql.DB, error) {
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	return sql.Open("sqlite", location.String())
}

func validateSQLite(ctx context.Context, path string) (resultErr error) {
	database, err := openArchivedSQLite(path)
	if err != nil {
		return fmt.Errorf("open restored SQLite snapshot: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("restored SQLite integrity check failed: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("restored SQLite integrity check failed: %s", integrity)
	}
	if err := store.ValidateSnapshotSchema(ctx, database); err != nil {
		return err
	}
	if err := validatePlatformTables(ctx, database); err != nil {
		return err
	}
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("check restored SQLite references: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	if rows.Next() {
		return errors.New("restored SQLite snapshot has broken foreign-key references")
	}
	return rows.Err()
}

func validatePlatformTables(ctx context.Context, database *sql.DB) error {
	for _, table := range []string{"groups", "nodes", "admin_users", "admin_sessions", "admin_audit_events"} {
		var count int
		err := database.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count)
		if err != nil {
			return fmt.Errorf("inspect restored SQLite table %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("restored SQLite table %s is missing", table)
		}
	}
	return nil
}
