package backup

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

const (
	manifestEntry  = "manifest.json"
	sqliteEntry    = "platform.sqlite"
	metricsEntry   = "metrics.jsonl.gz"
	dataKeyEntry   = "secrets/admin-data.key"
	checksumsEntry = "checksums.sha256"
)

type metricRecord struct {
	Measurement string                    `json:"measurement"`
	Tags        map[string]string         `json:"tags"`
	TimestampNS int64                     `json:"timestamp_ns"`
	Fields      map[string]mts.FieldValue `json:"fields"`
}

func (service *Service) createArchive(
	ctx context.Context,
	record model.BackupRecord,
) (model.BackupRecord, error) {
	temporary, err := os.MkdirTemp(service.root, ".backup-")
	if err != nil {
		return record, fmt.Errorf("create backup workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return record, fmt.Errorf("secure backup workspace: %w", err)
	}
	sqlitePath := filepath.Join(temporary, sqliteEntry)
	if err := service.sqlite.Snapshot(ctx, sqlitePath); err != nil {
		return record, err
	}
	cutoff := service.now()
	metricsPath := filepath.Join(temporary, metricsEntry)
	rows, err := service.writeMetrics(ctx, metricsPath, cutoff)
	if err != nil {
		return record, err
	}
	payloads := []string{sqliteEntry, metricsEntry}
	if err := service.copyDataKey(temporary); err != nil {
		return record, err
	} else if _, statErr := os.Stat(filepath.Join(temporary, dataKeyEntry)); statErr == nil {
		payloads = append(payloads, dataKeyEntry)
	} else if !os.IsNotExist(statErr) {
		return record, fmt.Errorf("inspect copied administrator key: %w", statErr)
	}
	manifest, err := service.buildManifest(temporary, payloads, cutoff, rows)
	if err != nil {
		return record, err
	}
	if err := writeMetadataFiles(temporary, manifest); err != nil {
		return record, err
	}
	archivePath, err := service.archivePath(record.Filename)
	if err != nil {
		return record, err
	}
	if err := writeArchive(archivePath+".partial", archivePath, temporary, payloads); err != nil {
		return record, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return record, fmt.Errorf("inspect completed backup: %w", err)
	}
	record.State = model.BackupStateReady
	record.SnapshotCutoff = &cutoff
	record.SizeBytes = info.Size()
	record.SQLiteBytes = manifest.PayloadSizes[sqliteEntry]
	record.MetricsBytes = manifest.PayloadSizes[metricsEntry]
	record.MetricRows = rows
	return record, nil
}

func (service *Service) writeMetrics(ctx context.Context, path string, cutoff time.Time) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create MTS export: %w", err)
	}
	gzipWriter := gzip.NewWriter(file)
	encoder := json.NewEncoder(gzipWriter)
	count, exportErr := service.mts.ExportRows(ctx, cutoff, func(row mts.Row) error {
		return encoder.Encode(metricRecord{Measurement: row.Measurement, Tags: row.Tags,
			TimestampNS: row.Timestamp, Fields: row.Fields})
	})
	closeGzipErr := gzipWriter.Close()
	syncErr := file.Sync()
	closeFileErr := file.Close()
	if err := errorsJoin(exportErr, closeGzipErr, syncErr, closeFileErr); err != nil {
		return count, fmt.Errorf("write MTS export: %w", err)
	}
	return count, nil
}

func (service *Service) copyDataKey(temporary string) error {
	source, err := os.Open(service.dataKeyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open administrator data key: %w", err)
	}
	defer func() { _ = source.Close() }()
	keyDirectory := filepath.Join(temporary, "secrets")
	if err := os.Mkdir(keyDirectory, 0o700); err != nil {
		return fmt.Errorf("create backup secrets directory: %w", err)
	}
	destination, err := os.OpenFile(filepath.Join(keyDirectory, "admin-data.key"),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup administrator key: %w", err)
	}
	_, copyErr := io.Copy(destination, io.LimitReader(source, 33))
	syncErr := destination.Sync()
	closeErr := destination.Close()
	if err := errorsJoin(copyErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("copy administrator data key: %w", err)
	}
	info, err := os.Stat(filepath.Join(keyDirectory, "admin-data.key"))
	if err != nil || info.Size() != 32 {
		return fmt.Errorf("administrator data key must contain exactly 32 bytes")
	}
	return nil
}

func (service *Service) buildManifest(
	root string,
	payloads []string,
	cutoff time.Time,
	rows int64,
) (model.BackupManifest, error) {
	manifest := model.BackupManifest{FormatVersion: 1, BeatVersion: service.version,
		CreatedAt: service.now(), SnapshotCutoff: cutoff, MetricRows: rows,
		PayloadSizes: map[string]int64{}, Checksums: map[string]string{},
		RequiredExternal: []string{"BEAT_ADMIN_TOKEN", "BEAT_AGENT_TOKEN", "TLS termination"}}
	for _, name := range payloads {
		size, checksum, err := fileDigest(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return model.BackupManifest{}, err
		}
		manifest.PayloadSizes[name] = size
		manifest.Checksums[name] = checksum
	}
	return manifest, nil
}

func writeMetadataFiles(root string, manifest model.BackupManifest) error {
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, manifestEntry), append(content, '\n'), 0o600); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	names := make([]string, 0, len(manifest.Checksums))
	for name := range manifest.Checksums {
		names = append(names, name)
	}
	sort.Strings(names)
	var checksums strings.Builder
	for _, name := range names {
		_, _ = fmt.Fprintf(&checksums, "%s  %s\n", manifest.Checksums[name], name)
	}
	if err := os.WriteFile(filepath.Join(root, checksumsEntry), []byte(checksums.String()), 0o600); err != nil {
		return fmt.Errorf("write backup checksums: %w", err)
	}
	return nil
}

func writeArchive(partial, destination, root string, payloads []string) error {
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup archive: %w", err)
	}
	entries := append([]string{manifestEntry}, payloads...)
	entries = append(entries, checksumsEntry)
	writer := zip.NewWriter(file)
	writeErr := addArchiveEntries(writer, root, entries)
	closeZipErr := writer.Close()
	syncErr := file.Sync()
	closeFileErr := file.Close()
	if err := errorsJoin(writeErr, closeZipErr, syncErr, closeFileErr); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("write backup archive: %w", err)
	}
	if err := os.Rename(partial, destination); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("publish backup archive: %w", err)
	}
	return os.Chmod(destination, 0o600)
}

func addArchiveEntries(writer *zip.Writer, root string, entries []string) error {
	for _, name := range entries {
		source, err := os.Open(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("open archive payload %s: %w", name, err)
		}
		destination, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err == nil {
			_, err = io.Copy(destination, source)
		}
		closeErr := source.Close()
		if joined := errorsJoin(err, closeErr); joined != nil {
			return fmt.Errorf("add archive payload %s: %w", name, joined)
		}
	}
	return nil
}

func fileDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("open payload for checksum: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errorsJoin(copyErr, closeErr); err != nil {
		return 0, "", fmt.Errorf("checksum backup payload: %w", err)
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func backupFilename(now time.Time, id string) string {
	return fmt.Sprintf("beat-backup-v1-%s-%s.zip", now.UTC().Format("20060102T150405Z"), id[:8])
}

func errorsJoin(values ...error) error {
	return errors.Join(values...)
}
