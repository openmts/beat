package store

import (
	"context"
	"fmt"
	"os"
)

func (s *SQLiteStore) Snapshot(ctx context.Context, destination string) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("SQLite snapshot destination already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect SQLite snapshot destination: %w", err)
	}
	if err := s.checkpoint(ctx); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return fmt.Errorf("secure SQLite snapshot: %w", err)
	}
	return nil
}
