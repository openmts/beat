package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/store"
)

type preparedRestore struct {
	targets     []restoreTarget
	mtsLive     string
	metricsPath string
	committed   bool
}

type restoreTarget struct {
	live string
	new  string
}

func prepareRestore(
	validated *validatedArchive,
	dbPath string,
	mtsPath string,
	dataDir string,
	token string,
) (*preparedRestore, error) {
	prepared := &preparedRestore{mtsLive: mtsPath,
		metricsPath: filepath.Join(validated.root, metricsEntry),
		targets:     []restoreTarget{{live: dbPath, new: dbPath + ".restore-new-" + token}}}
	if err := copyFile(filepath.Join(validated.root, sqliteEntry), prepared.targets[0].new, 0o600); err != nil {
		prepared.cleanup()
		return nil, err
	}
	keySource := filepath.Join(validated.root, filepath.FromSlash(dataKeyEntry))
	if _, err := os.Stat(keySource); err == nil {
		keyTarget := restoreTarget{live: filepath.Join(dataDir, "admin-data.key"),
			new: filepath.Join(dataDir, "admin-data.key.restore-new-"+token)}
		if err := copyFile(keySource, keyTarget.new, 0o600); err != nil {
			prepared.cleanup()
			return nil, err
		}
		prepared.targets = append(prepared.targets, keyTarget)
	} else if !os.IsNotExist(err) {
		prepared.cleanup()
		return nil, fmt.Errorf("inspect restored administrator key: %w", err)
	}
	return prepared, nil
}

func importMetrics(ctx context.Context, path, destination string) error {
	if err := os.Mkdir(destination, 0o700); err != nil {
		return fmt.Errorf("create restored MTS directory: %w", err)
	}
	mtsStore, err := store.NewMTSStore(destination)
	if err != nil {
		return err
	}
	importErr := replayMetrics(ctx, path, mtsStore)
	flushErr := mtsStore.Flush(ctx)
	healthy, reasons := mtsStore.Health()
	closeErr := mtsStore.Close()
	if !healthy {
		importErr = errors.Join(importErr, fmt.Errorf("restored MTS is unhealthy: %s", strings.Join(reasons, ", ")))
	}
	if err := errors.Join(importErr, flushErr, closeErr); err != nil {
		return fmt.Errorf("materialize restored MTS: %w", err)
	}
	return nil
}

func replayMetrics(ctx context.Context, path string, destination *store.MTSStore) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open restored MTS export: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open restored MTS gzip: %w", err)
	}
	defer func() { _ = reader.Close() }()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumJSONLLine)
	batch := make([]mts.Point, 0, 500)
	for scanner.Scan() {
		var record metricRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode restored MTS row: %w", err)
		}
		batch = append(batch, mts.Point{Measurement: record.Measurement, Tags: record.Tags,
			Timestamp: record.TimestampNS, Precision: mts.PrecisionNanosecond, Fields: record.Fields})
		if len(batch) == cap(batch) {
			if err := destination.ImportPoints(ctx, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read restored MTS rows: %w", err)
	}
	return destination.ImportPoints(ctx, batch)
}

func validateApplied(ctx context.Context, dbPath, mtsPath string) error {
	if err := validateSQLite(ctx, dbPath); err != nil {
		return err
	}
	mtsStore, err := store.NewMTSStore(mtsPath)
	if err != nil {
		return err
	}
	healthy, reasons := mtsStore.Health()
	closeErr := mtsStore.Close()
	if !healthy {
		return errors.Join(fmt.Errorf("restored MTS health check failed: %s", strings.Join(reasons, ", ")), closeErr)
	}
	return closeErr
}

func copyFile(sourcePath, destinationPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open restore source: %w", err)
	}
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("create restore target: %w", err)
	}
	_, copyErr := io.Copy(destination, source)
	syncErr := destination.Sync()
	closeDestinationErr := destination.Close()
	closeSourceErr := source.Close()
	if err := errors.Join(copyErr, syncErr, closeDestinationErr, closeSourceErr); err != nil {
		return fmt.Errorf("copy restore target: %w", err)
	}
	return nil
}

func (prepared *preparedRestore) cleanup() {
	if prepared.committed {
		return
	}
	for _, target := range prepared.targets {
		_ = os.RemoveAll(target.new)
	}
}
