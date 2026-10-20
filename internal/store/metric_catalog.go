package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

const metricCatalogWriteTimeout = 5 * time.Second

const metricDefinitionColumns = `metric_key,label,description,source,kind,unit,aggregations_json,default_aggregation,
	public,enabled,retention_days,COALESCE(retention_days,(SELECT retention_days FROM maintenance_settings WHERE id=1),30),
	revision,minimum,maximum,tags_json,created_at,updated_at`

var ErrMetricCatalogConflict = errors.New("metric policy revision has changed")

var ErrMetricCatalogNotFound = errors.New("metric definition not found")

type MetricCatalogStore struct{ db *sql.DB }

func NewMetricCatalogStore(db *sql.DB) *MetricCatalogStore { return &MetricCatalogStore{db: db} }

func (catalog *MetricCatalogStore) Get(ctx context.Context, key string) (*model.MetricDefinition, error) {
	return findMetricDefinition(ctx, catalog.db, key)
}

func findMetricDefinition(ctx context.Context, db schemaExecutor, key string) (*model.MetricDefinition, error) {
	definition, err := scanMetricDefinition(db.QueryRowContext(ctx, "SELECT "+metricDefinitionColumns+" FROM metric_definitions WHERE metric_key=?", key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return definition, err
}

type metricDefinitionScanner interface{ Scan(...any) error }

func scanMetricDefinition(row metricDefinitionScanner) (*model.MetricDefinition, error) {
	var definition model.MetricDefinition
	var aggregations, tags string
	var override sql.NullInt64
	err := row.Scan(&definition.Key, &definition.Label, &definition.Description, &definition.Source, &definition.Kind, &definition.Unit,
		&aggregations, &definition.DefaultAggregation, &definition.Public, &definition.Enabled, &override, &definition.RetentionDays,
		&definition.Revision, &definition.Minimum, &definition.Maximum, &tags, &definition.CreatedAt, &definition.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(aggregations), &definition.Aggregations); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(tags), &definition.Tags); err != nil {
		return nil, err
	}
	if override.Valid {
		value := int(override.Int64)
		definition.RetentionOverride = &value
	}
	if err := metriccatalog.ValidateDefinition(definition); err != nil {
		return nil, err
	}
	if err := validateStoredMetricSemantics(definition); err != nil {
		return nil, err
	}
	return &definition, nil
}

func (catalog *MetricCatalogStore) Catalog(ctx context.Context, publicOnly bool) (model.MetricCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, metricCatalogWriteTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return model.MetricCatalog{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var result model.MetricCatalog
	err = tx.QueryRowContext(ctx, "SELECT version,revision FROM metric_catalog_state WHERE id=1").Scan(&result.Version, &result.Revision)
	if err != nil {
		return result, err
	}
	if result.Version != metriccatalog.Version {
		return result, errors.New("unsupported metric catalog version")
	}
	result.Metrics, err = listMetricDefinitions(ctx, tx, publicOnly)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func listMetricDefinitions(ctx context.Context, db schemaExecutor, publicOnly bool) ([]model.MetricDefinition, error) {
	query := "SELECT " + metricDefinitionColumns + " FROM metric_definitions"
	if publicOnly {
		query += " WHERE public=1 AND enabled=1"
	}
	rows, err := db.QueryContext(ctx, query+" ORDER BY display_order,metric_key LIMIT ?", metriccatalog.MaximumDefinitions+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	definitions := make([]model.MetricDefinition, 0)
	for rows.Next() {
		definition, err := scanMetricDefinition(rows)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, *definition)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(definitions) > metriccatalog.MaximumDefinitions {
		return nil, fmt.Errorf("metric catalog exceeds %d definitions", metriccatalog.MaximumDefinitions)
	}
	return definitions, nil
}

func (catalog *MetricCatalogStore) Reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, metricCatalogWriteTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reconcileMetricCatalog(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
