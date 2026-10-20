package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/beat/backend/internal/store"
)

func invalidateRestoredAuthentication(ctx context.Context, path string, restore store.RestoredAuthentication) (resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return fmt.Errorf("open private authentication snapshot: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	if err := store.InvalidateRestoredAuthentication(ctx, database, restore); err != nil {
		return fmt.Errorf("invalidate restored authentication: %w", err)
	}
	var busy, frames, checkpointed int
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("authentication snapshot checkpoint is busy")
	}
	return nil
}
