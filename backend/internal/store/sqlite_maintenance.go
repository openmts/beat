package store

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func (s *SQLiteStore) Maintain(ctx context.Context) (string, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := s.checkpoint(ctx); err != nil {
		return "", err
	}
	integrity, err := s.integrityCheck(ctx)
	if err != nil {
		return integrity, err
	}
	if _, err := s.DB.ExecContext(ctx, "VACUUM"); err != nil {
		return integrity, fmt.Errorf("vacuum SQLite: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return integrity, fmt.Errorf("optimize SQLite: %w", err)
	}
	return integrity, nil
}

func (s *SQLiteStore) checkpoint(ctx context.Context) error {
	var busy, logFrames, checkpointed int
	err := s.DB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(
		&busy, &logFrames, &checkpointed,
	)
	if err != nil {
		return fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}
	if busy != 0 {
		return fmt.Errorf("checkpoint SQLite WAL: database is busy")
	}
	return nil
}

func (s *SQLiteStore) integrityCheck(ctx context.Context) (string, error) {
	var integrity string
	if err := s.DB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return "", fmt.Errorf("check SQLite integrity: %w", err)
	}
	if integrity != "ok" {
		return integrity, fmt.Errorf("check SQLite integrity: %s", integrity)
	}
	return integrity, nil
}

func (s *SQLiteStore) DiskUsage() (int64, error) {
	path := sqliteFilesystemPath(s.path)
	if path == "" {
		return 0, nil
	}
	var total int64
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("measure SQLite disk usage: %w", err)
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total, nil
}

func sqliteFilesystemPath(path string) string {
	if strings.Contains(path, "mode=memory") {
		return ""
	}
	path = strings.TrimPrefix(path, "file:")
	if index := strings.IndexByte(path, '?'); index >= 0 {
		path = path[:index]
	}
	if path == ":memory:" {
		return ""
	}
	return path
}
