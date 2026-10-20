package backup

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type archiveCapture struct {
	payloads []string
	cutoff   time.Time
	rows     int64
	themes   map[string]model.BackupThemeFile
	geoip    map[string]model.MMDBMetadata
}

func (service *Service) captureArchive(ctx context.Context, temporary string) (archiveCapture, error) {
	var captured archiveCapture
	capture := func(assets []geoip.BackupAsset) error {
		var err error
		captured, err = service.capturePlatformData(ctx, temporary)
		if err != nil {
			return err
		}
		captured.payloads, captured.themes, err = service.archivePayloads(temporary)
		if err != nil {
			return err
		}
		return captureGeoIPAssets(temporary, assets, &captured)
	}
	var err error
	if service.geoipAssets != nil {
		err = service.geoipAssets.WithBackupAssets(ctx, capture)
	} else {
		err = capture(nil)
	}
	return captured, err
}

func (service *Service) capturePlatformData(ctx context.Context, temporary string) (archiveCapture, error) {
	captured := archiveCapture{}
	snapshot, err := service.mts.BeginMetricSnapshot(ctx)
	if err != nil {
		return captured, err
	}
	defer snapshot.Close()
	sqlitePath := filepath.Join(temporary, sqliteEntry)
	if err := service.sqlite.Snapshot(ctx, sqlitePath); err != nil {
		return captured, err
	}
	if err := invalidateEnrollmentCredentials(ctx, sqlitePath); err != nil {
		return captured, err
	}
	captured.cutoff = service.now()
	metricsPath := filepath.Join(temporary, metricsEntry)
	captured.rows, err = service.writeSnapshotMetrics(ctx, metricArchiveInput{path: metricsPath, cutoff: captured.cutoff}, snapshot.ExportCatalogRows)
	return captured, err
}

func (service *Service) writeMetrics(ctx context.Context, path string, cutoff time.Time) (int64, error) {
	return service.writeSnapshotMetrics(ctx, metricArchiveInput{path: path, cutoff: cutoff}, service.mts.ExportCatalogRows)
}

type metricArchiveInput struct {
	path   string
	cutoff time.Time
}

type metricArchiveExporter func(context.Context, store.MetricExport, store.MTSRowConsumer) (int64, error)

func (service *Service) writeSnapshotMetrics(ctx context.Context, input metricArchiveInput, export metricArchiveExporter) (int64, error) {
	catalog, err := snapshotMetricCatalog(ctx, filepath.Join(filepath.Dir(input.path), sqliteEntry))
	if err != nil {
		return 0, err
	}
	end, err := model.AgentArchiveEnd(input.cutoff)
	if err != nil {
		return 0, err
	}
	file, err := os.OpenFile(input.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create MTS export: %w", err)
	}
	gzipWriter := gzip.NewWriter(file)
	encoder := json.NewEncoder(gzipWriter)
	count, exportErr := export(ctx, store.MetricExport{Cutoff: end, Definitions: catalog.Metrics}, func(row mts.Row) error {
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
