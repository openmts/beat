package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/store"
)

const acceleratorArchiveBatch = 500

type acceleratorArchiveMemory struct {
	parent, root string
	database     *store.MTSStore
	batch        []mts.Point
}

func (check *acceleratorArchiveMemory) add(ctx context.Context, row metricRecord) error {
	if row.Measurement != "gpu_memory_used_bytes" && row.Measurement != "gpu_memory_total_bytes" {
		return nil
	}
	if check.database == nil {
		if err := check.open(); err != nil {
			return err
		}
	}
	check.batch = append(check.batch, mts.Point{Measurement: row.Measurement, Tags: row.Tags, Timestamp: row.TimestampNS, Precision: mts.PrecisionNanosecond, Fields: row.Fields})
	if len(check.batch) == acceleratorArchiveBatch {
		return check.flush(ctx)
	}
	return nil
}

func (check *acceleratorArchiveMemory) open() error {
	root, err := os.MkdirTemp(check.parent, ".gpu-archive-check-")
	if err != nil {
		return err
	}
	check.root = root
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(root, "mts"), 0o700); err != nil {
		return err
	}
	database, err := store.NewMTSStore(filepath.Join(root, "mts"))
	if err != nil {
		return err
	}
	check.database = database
	check.batch = make([]mts.Point, 0, acceleratorArchiveBatch)
	return nil
}

func (check *acceleratorArchiveMemory) flush(ctx context.Context) error {
	if err := check.database.ImportPoints(ctx, check.batch); err != nil {
		return err
	}
	check.batch = check.batch[:0]
	return nil
}

func (check *acceleratorArchiveMemory) finish(ctx context.Context) error {
	if check.database == nil {
		return nil
	}
	if err := check.flush(ctx); err != nil {
		return err
	}
	if err := check.database.Flush(ctx); err != nil {
		return err
	}
	return check.database.ValidateAcceleratorArchiveMemory(ctx)
}

func (check *acceleratorArchiveMemory) close() error {
	var err error
	if check.database != nil {
		err = check.database.Close()
	}
	if check.root != "" {
		err = errors.Join(err, os.RemoveAll(check.root))
	}
	return err
}
