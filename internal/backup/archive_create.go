package backup

import (
	"archive/zip"
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
	captured, err := service.captureArchive(ctx, temporary)
	if err != nil {
		return record, err
	}
	manifest, err := service.buildManifest(temporary, captured.payloads, captured.cutoff, captured.rows, captured.themes)
	if err != nil {
		return record, err
	}
	manifest.GeoIPFiles = captured.geoip
	manifest.ExternalMMDB = service.externalMMDB
	if service.externalMMDB != "" {
		manifest.RequiredExternal = append(manifest.RequiredExternal, service.externalMMDB)
	}
	if err := validateGeoIPPayloads(temporary, manifest); err != nil {
		return record, err
	}
	if err := writeMetadataFiles(temporary, manifest); err != nil {
		return record, err
	}
	archivePath, err := service.archivePath(record.Filename)
	if err != nil {
		return record, err
	}
	if err := writeArchive(archivePath+".partial", archivePath, temporary, captured.payloads); err != nil {
		return record, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return record, fmt.Errorf("inspect completed backup: %w", err)
	}
	record.State, record.SnapshotCutoff, record.SizeBytes = model.BackupStateReady, &captured.cutoff, info.Size()
	record.SQLiteBytes, record.MetricsBytes, record.MetricRows = manifest.PayloadSizes[sqliteEntry], manifest.PayloadSizes[metricsEntry], captured.rows
	return record, nil
}

func (service *Service) archivePayloads(temporary string) ([]string, map[string]model.BackupThemeFile, error) {
	payloads := []string{sqliteEntry, metricsEntry}
	if err := service.copyDataKey(temporary); err != nil {
		return nil, nil, err
	} else if _, statErr := os.Stat(filepath.Join(temporary, dataKeyEntry)); statErr == nil {
		payloads = append(payloads, dataKeyEntry)
	} else if !os.IsNotExist(statErr) {
		return nil, nil, fmt.Errorf("inspect copied administrator key: %w", statErr)
	}
	themeFiles := map[string]model.BackupThemeFile{}
	if service.themesDir != "" {
		themePayloads, err := service.copyThemes(temporary, themeFiles)
		if err != nil {
			return nil, nil, err
		}
		payloads = append(payloads, themePayloads...)
	}
	return payloads, themeFiles, nil
}

func isHexString(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if !isHexCharacter(byte(character)) {
			return false
		}
	}
	return true
}

func isHexCharacter(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'f'
}

func (service *Service) buildManifest(
	root string,
	payloads []string,
	cutoff time.Time,
	rows int64,
	themeFiles map[string]model.BackupThemeFile,
) (model.BackupManifest, error) {
	ctx := service.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	catalog, err := snapshotMetricCatalog(ctx, filepath.Join(root, sqliteEntry))
	if err != nil {
		return model.BackupManifest{}, err
	}
	manifest := model.BackupManifest{FormatVersion: 4, BeatVersion: service.version,
		CreatedAt: service.now(), SnapshotCutoff: cutoff, MetricRows: rows,
		CatalogVersion: catalog.Version, CatalogRevision: catalog.Revision,
		PayloadSizes: map[string]int64{}, Checksums: map[string]string{},
		RequiredExternal: []string{"BEAT_ADMIN_TOKEN", "BEAT_AGENT_TOKEN", "TLS termination"},
		ThemeFiles:       themeFiles}
	manifest.AgentReleaseCache, err = snapshotAgentReleaseCache(ctx, filepath.Join(root, sqliteEntry))
	if err != nil {
		return model.BackupManifest{}, err
	}
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
	content = append(content, '\n')
	if len(content) > maximumManifestBytes {
		return errors.New("backup manifest exceeds its limit")
	}
	if err := os.WriteFile(filepath.Join(root, manifestEntry), content, 0o600); err != nil {
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
	return fmt.Sprintf("beat-backup-v4-%s-%s.zip", now.UTC().Format("20060102T150405Z"), id[:8])
}

func errorsJoin(values ...error) error {
	return errors.Join(values...)
}
