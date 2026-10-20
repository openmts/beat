package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func snapshotMetricCatalog(ctx context.Context, path string) (catalog model.MetricCatalog, resultErr error) {
	database, err := openArchivedSQLite(path)
	if err != nil {
		return catalog, err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	catalog, err = store.NewMetricCatalogStore(database).Catalog(ctx, false)
	if err != nil {
		return catalog, fmt.Errorf("read archived metric catalog: %w", err)
	}
	available := make(map[string]bool, len(catalog.Metrics))
	for _, definition := range catalog.Metrics {
		available[definition.Key] = true
	}
	var schemaVersion int
	if err := database.QueryRowContext(ctx, "SELECT max(version) FROM schema_migrations").Scan(&schemaVersion); err != nil {
		return catalog, err
	}
	for _, definition := range metriccatalog.Builtins() {
		if schemaVersion < 14 && model.ValidAcceleratorMetric(definition.Key) {
			continue
		}
		if !available[definition.Key] {
			return catalog, errors.New("archived metric catalog is missing a builtin definition")
		}
	}
	return catalog, nil
}

func archivedMetricInventory(ctx context.Context, root string, manifest model.BackupManifest) (map[string]model.MetricDefinition, error) {
	if manifest.FormatVersion < 3 {
		return nil, nil
	}
	catalog, err := snapshotMetricCatalog(ctx, filepath.Join(root, sqliteEntry))
	if err != nil {
		return nil, err
	}
	if catalog.Version != manifest.CatalogVersion || catalog.Revision != manifest.CatalogRevision {
		return nil, errors.New("archived metric catalog does not match manifest")
	}
	definitions := make(map[string]model.MetricDefinition, len(catalog.Metrics))
	for _, definition := range catalog.Metrics {
		definitions[definition.Key] = definition
	}
	return definitions, nil
}
