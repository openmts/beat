package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func (s *MTSStore) AttachMetricCatalog(ctx context.Context, catalog *MetricCatalogStore) error {
	if catalog == nil {
		return errors.New("metric catalog is required")
	}
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if s.catalog != nil && s.catalog.db == catalog.db {
		return nil
	}
	names, err := s.engine.ListMeasurements(ctx, "beat")
	if err != nil {
		return err
	}
	if len(names) > metriccatalog.MaximumDefinitions+3 {
		return errors.New("stored measurement inventory exceeds its limit")
	}
	definitions, err := s.recoverMetricInventory(ctx, catalog, names)
	if err != nil {
		return err
	}
	if err := catalog.RegisterHistory(ctx, definitions); err != nil {
		return err
	}
	s.catalog = catalog
	return nil
}

func (s *MTSStore) recoverMetricInventory(ctx context.Context, catalog *MetricCatalogStore, names []string) ([]model.MetricDefinition, error) {
	definitions := make([]model.MetricDefinition, 0)
	for _, name := range names {
		if name == model.AgentReceiptMeasurement {
			if err := s.validateAgentReceiptSchema(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if name == model.AcceleratorSampleMeasurement {
			if err := s.validateAcceleratorSampleSchema(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if name == networkProbeMeasurement {
			continue
		}
		definition, err := catalog.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		if err := s.validateScalarMeasurement(ctx, name); err != nil {
			return nil, err
		}
		if definition != nil {
			continue
		}
		if !strings.HasPrefix(name, model.CustomMetricPrefix) {
			return nil, fmt.Errorf("unregistered stored measurement %q", name)
		}
		recovered, err := metriccatalog.Custom(strings.TrimPrefix(name, model.CustomMetricPrefix), "Recovered historical metric")
		if err != nil {
			return nil, err
		}
		recovered.Public = false
		definitions = append(definitions, recovered)
	}
	return definitions, nil
}

func (s *MTSStore) validateAgentReceiptSchema(ctx context.Context) error {
	fields, err := s.engine.ListFields(ctx, "beat", model.AgentReceiptMeasurement)
	if err != nil {
		return err
	}
	expected := AgentReceiptFields()
	if len(fields) != len(expected) {
		return ErrAgentReceiptInvalid
	}
	for _, field := range fields {
		kind, ok := expected[field.Name]
		if !ok || kind != field.Type {
			return ErrAgentReceiptInvalid
		}
	}
	return nil
}

func (s *MTSStore) validateScalarMeasurement(ctx context.Context, name string) error {
	fields, err := s.engine.ListFields(ctx, "beat", name)
	if err != nil {
		return err
	}
	if len(fields) != 1 || fields[0].Name != "value" || fields[0].Type != mts.FieldFloat64 {
		return fmt.Errorf("measurement %q has an incompatible field schema", name)
	}
	return nil
}

func (catalog *MetricCatalogStore) RegisterHistory(ctx context.Context, definitions []model.MetricDefinition) error {
	if len(definitions) > metriccatalog.MaximumDefinitions {
		return errors.New("historical metric inventory exceeds its limit")
	}
	ctx, cancel := context.WithTimeout(ctx, metricCatalogWriteTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, definition := range definitions {
		if err := ensureMetricDefinition(ctx, tx, definition); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *MTSStore) validateAcceleratorSampleSchema(ctx context.Context) error {
	fields, err := s.engine.ListFields(ctx, "beat", model.AcceleratorSampleMeasurement)
	if err != nil {
		return err
	}
	expected := AcceleratorSampleFields()
	if len(fields) != len(expected) {
		return ErrAgentReceiptInvalid
	}
	for _, field := range fields {
		kind, ok := expected[field.Name]
		if !ok || kind != field.Type {
			return ErrAgentReceiptInvalid
		}
	}
	return nil
}
