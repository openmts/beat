package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sqlite3 "modernc.org/sqlite"
)

const (
	sqliteBusyCode   = 5
	sqliteLockedCode = 6
	writeRetryDelay  = 10 * time.Millisecond
)

func beginWriteTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	for {
		tx, err := db.BeginTx(ctx, nil)
		if err == nil || !isSQLiteLockError(err) {
			return tx, err
		}
		if err := waitForSQLiteRetry(ctx); err != nil {
			return nil, err
		}
	}
}

func waitForSQLiteRetry(ctx context.Context) error {
	timer := time.NewTimer(writeRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isSQLiteLockError(err error) bool {
	var sqliteErr *sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	baseCode := sqliteErr.Code() & 0xff
	return baseCode == sqliteBusyCode || baseCode == sqliteLockedCode
}
