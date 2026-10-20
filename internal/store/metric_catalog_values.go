package store

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/metriccatalog"
)

func (catalog *MetricCatalogStore) BuiltinPersistence(ctx context.Context) (map[string]bool, error) {
	return builtinPersistenceFrom(ctx, catalog.db)
}

func builtinPersistenceFrom(ctx context.Context, db schemaExecutor) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT metric_key,enabled FROM metric_definitions WHERE source IN ('builtin','derived') LIMIT ?", len(metriccatalog.Builtins())+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	enabled := make(map[string]bool)
	for rows.Next() {
		var key string
		var active bool
		if err := rows.Scan(&key, &active); err != nil {
			return nil, err
		}
		enabled[key] = active
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, definition := range metriccatalog.Builtins() {
		if _, ok := enabled[definition.Key]; !ok {
			return nil, fmt.Errorf("builtin metric policy %q is missing", definition.Key)
		}
	}
	if len(enabled) != len(metriccatalog.Builtins()) {
		return nil, fmt.Errorf("builtin metric inventory is incompatible")
	}
	return enabled, nil
}

func (catalog *MetricCatalogStore) PublicKeys(ctx context.Context) (map[string]bool, error) {
	rows, err := catalog.db.QueryContext(ctx, "SELECT metric_key FROM metric_definitions WHERE public=1 AND enabled=1 LIMIT ?", metriccatalog.MaximumDefinitions+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(keys) > metriccatalog.MaximumDefinitions {
		return nil, fmt.Errorf("metric catalog exceeds its limit")
	}
	return keys, nil
}
