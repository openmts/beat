package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"
)

const enrollmentSnapshotTimeout = 5 * time.Second

// 只处理私有备份/恢复副本，避免旧快照重新激活已经使用的接入凭据。
func invalidateEnrollmentCredentials(ctx context.Context, path string) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, enrollmentSnapshotTimeout)
	defer cancel()
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return fmt.Errorf("open private enrollment snapshot: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var count int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_enrollment_tokens'").Scan(&count)
	if err != nil {
		return fmt.Errorf("inspect enrollment snapshot: %w", err)
	}
	if count == 0 {
		return nil
	}
	if _, err := database.ExecContext(ctx, "UPDATE agent_enrollment_tokens SET revoked_at = ? WHERE revoked_at IS NULL", time.Now().UTC()); err != nil {
		return fmt.Errorf("invalidate restored enrollment credentials: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("checkpoint enrollment snapshot: %w", err)
	}
	if busy != 0 {
		return errors.New("enrollment snapshot checkpoint is busy")
	}
	return nil
}
