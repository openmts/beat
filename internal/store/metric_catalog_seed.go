package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func reconcileMetricCatalog(ctx context.Context, db schemaExecutor) error {
	var version int
	if err := db.QueryRowContext(ctx, "SELECT version FROM metric_catalog_state WHERE id=1").Scan(&version); err != nil {
		return err
	}
	if version != metriccatalog.Version {
		return errors.New("unsupported metric catalog version")
	}
	for _, definition := range metriccatalog.Builtins() {
		if err := ensureMetricDefinition(ctx, db, definition); err != nil {
			return err
		}
	}
	if err := reconcileCustomMetricDefinitions(ctx, db); err != nil {
		return err
	}
	_, err := listMetricDefinitions(ctx, db, false)
	return err
}

func validateStoredMetricSemantics(definition model.MetricDefinition) error {
	if definition.Source == "custom" {
		if !strings.HasPrefix(definition.Key, model.CustomMetricPrefix) {
			return metriccatalog.ErrInvalidDefinition
		}
		expected, err := metriccatalog.Custom(strings.TrimPrefix(definition.Key, model.CustomMetricPrefix), "")
		if err != nil {
			return err
		}
		if metriccatalog.SameSemantics(expected, definition) {
			return nil
		}
	} else {
		for _, expected := range metriccatalog.Builtins() {
			if metriccatalog.SameSemantics(expected, definition) {
				return nil
			}
		}
	}
	return fmt.Errorf("metric %q has incompatible persisted semantics", definition.Key)
}

func reconcileCustomMetricDefinitions(ctx context.Context, db schemaExecutor) error {
	rows, err := db.QueryContext(ctx, "SELECT name,description FROM custom_metrics ORDER BY name LIMIT ?", metriccatalog.MaximumDefinitions+1)
	if err != nil {
		return err
	}
	definitions := make([]model.MetricDefinition, 0)
	for rows.Next() {
		var name, description string
		if err := rows.Scan(&name, &description); err != nil {
			_ = rows.Close()
			return err
		}
		definition, err := customCatalogDefinition(name, description)
		if err != nil {
			_ = rows.Close()
			return err
		}
		definitions = append(definitions, definition)
	}
	readErr, closeErr := rows.Err(), rows.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	for _, definition := range definitions {
		if err := ensureMetricDefinition(ctx, db, definition); err != nil {
			return err
		}
	}
	return nil
}

func customCatalogDefinition(name, description string) (model.MetricDefinition, error) {
	if len(description) > metriccatalog.MaximumDescriptionBytes {
		description = description[:metriccatalog.MaximumDescriptionBytes]
		for !utf8.ValidString(description) && len(description) > 0 {
			description = description[:len(description)-1]
		}
	}
	return metriccatalog.Custom(name, description)
}

func ensureMetricDefinition(ctx context.Context, db schemaExecutor, definition model.MetricDefinition) error {
	if err := metriccatalog.ValidateDefinition(definition); err != nil {
		return err
	}
	if err := validateStoredMetricSemantics(definition); err != nil {
		return err
	}
	stored, err := findMetricDefinition(ctx, db, definition.Key)
	if err != nil {
		return err
	}
	if stored != nil {
		if !metriccatalog.SameSemantics(*stored, definition) {
			return fmt.Errorf("metric %q has incompatible persisted semantics", definition.Key)
		}
		return nil
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metric_definitions").Scan(&count); err != nil {
		return err
	}
	if count >= metriccatalog.MaximumDefinitions {
		return fmt.Errorf("metric catalog exceeds %d definitions", metriccatalog.MaximumDefinitions)
	}
	return insertMetricDefinition(ctx, db, definition)
}

func insertMetricDefinition(ctx context.Context, db schemaExecutor, definition model.MetricDefinition) error {
	aggregations, err := json.Marshal(definition.Aggregations)
	if err != nil {
		return err
	}
	tags, err := json.Marshal(definition.Tags)
	if err != nil {
		return err
	}
	now := model.NowUTC()
	_, err = db.ExecContext(ctx, `INSERT INTO metric_definitions(metric_key,label,description,source,kind,unit,aggregations_json,
		default_aggregation,public,enabled,retention_days,revision,minimum,maximum,tags_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,NULL,1,?,?,?,?,?)`, definition.Key, definition.Label, definition.Description, definition.Source,
		definition.Kind, definition.Unit, string(aggregations), definition.DefaultAggregation, boolInt(definition.Public),
		boolInt(definition.Enabled), definition.Minimum, definition.Maximum, string(tags), now, now)
	return err
}
