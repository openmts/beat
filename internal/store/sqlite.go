package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/beat/backend/internal/model"
)

type SQLiteStore struct {
	DB          *sql.DB
	path        string
	operationMu sync.Mutex
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	return NewSQLiteStoreContext(context.Background(), dbPath)
}

func NewSQLiteStoreContext(ctx context.Context, dbPath string) (*SQLiteStore, error) {
	if err := secureSQLiteFile(dbPath); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteConnectionString(dbPath))
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}
	if err := enableSQLiteWAL(ctx, db, dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating sqlite database: %w", err)
	}
	if err := secureSQLiteFiles(dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{DB: db, path: dbPath}, nil
}

func secureSQLiteFile(path string) error {
	filesystemPath := sqliteFilesystemPath(path)
	if filesystemPath == "" {
		return nil
	}
	file, err := os.OpenFile(filesystemPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("secure SQLite database: %w", err)
	}
	chmodErr := file.Chmod(0o600)
	closeErr := file.Close()
	if chmodErr != nil {
		return fmt.Errorf("secure SQLite database permissions: %w", chmodErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close secured SQLite database: %w", closeErr)
	}
	return nil
}

func secureSQLiteFiles(path string) error {
	filesystemPath := sqliteFilesystemPath(path)
	if filesystemPath == "" {
		return nil
	}
	for _, candidate := range []string{filesystemPath, filesystemPath + "-wal", filesystemPath + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("secure SQLite file %q: %w", candidate, err)
		}
	}
	return nil
}

func enableSQLiteWAL(ctx context.Context, db *sql.DB, path string) error {
	if sqliteFilesystemPath(path) == "" {
		return nil
	}
	for {
		var mode string
		err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode)
		if err == nil {
			if !strings.EqualFold(mode, "wal") {
				return fmt.Errorf("enable SQLite WAL: journal mode is %q", mode)
			}
			return nil
		}
		if !isSQLiteLockError(err) {
			return fmt.Errorf("enable SQLite WAL: %w", err)
		}
		if err := waitForSQLiteRetry(ctx); err != nil {
			return fmt.Errorf("enable SQLite WAL: %w", err)
		}
	}
}

func (s *SQLiteStore) Ready(ctx context.Context) error {
	if err := s.DB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}
	var version sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("query sqlite schema version: %w", err)
	}
	if !version.Valid || version.Int64 != currentSchemaVersion {
		return fmt.Errorf("sqlite schema version is %d, want %d", version.Int64, currentSchemaVersion)
	}
	return nil
}

func sqliteConnectionString(path string) string {
	if path == ":memory:" {
		path = "file:beat-" + uuid.NewString() + "?mode=memory&cache=shared"
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"
}

type schemaExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func createTables(ctx context.Context, db schemaExecutor) error {
	if err := executeSchemaStatements(ctx, db, platformSchemaStatements); err != nil {
		return err
	}
	return executeSchemaStatements(ctx, db, adminSchemaStatements)
}

func executeSchemaStatements(ctx context.Context, db schemaExecutor, statements []string) error {
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("executing ddl: %w", err)
		}
	}
	return nil
}

func ensureDefaultGroup(ctx context.Context, db schemaExecutor) error {
	rows, err := db.QueryContext(ctx, "SELECT id FROM groups WHERE is_default = 1 ORDER BY sort_order, id")
	if err != nil {
		return fmt.Errorf("query default groups: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan default group: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close default groups: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate default groups: %w", err)
	}
	if len(ids) > 1 {
		if _, err := db.ExecContext(ctx, "UPDATE groups SET is_default = 0 WHERE is_default = 1 AND id <> ?", ids[0]); err != nil {
			return fmt.Errorf("normalize default groups: %w", err)
		}
	}
	defaultID := ""
	if len(ids) > 0 {
		defaultID = ids[0]
	}
	if defaultID == "" {
		defaultID = uuid.NewString()
		if err := insertDefaultGroup(ctx, db, defaultID); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE nodes SET group_id = ?
		WHERE group_id IS NULL OR group_id = '' OR NOT EXISTS (
			SELECT 1 FROM groups WHERE groups.id = nodes.group_id
		)`, defaultID); err != nil {
		return fmt.Errorf("repair node groups: %w", err)
	}
	return nil
}

func insertDefaultGroup(ctx context.Context, db schemaExecutor, id string) error {
	now := model.NowUTC()
	_, err := db.ExecContext(ctx,
		"INSERT INTO groups (id, name, sort_order, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, "Default", 0, 1, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting default group: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.DB.Close()
}
