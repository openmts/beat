package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

type MTSRowConsumer func(mts.Row) error

func (s *MTSStore) ExportRows(
	ctx context.Context,
	cutoff time.Time,
	consume MTSRowConsumer,
) (int64, error) {
	if consume == nil {
		return 0, errors.New("MTS row consumer is required")
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := s.engine.Flush(ctx); err != nil {
		return 0, fmt.Errorf("flush MTS before export: %w", err)
	}
	var count int64
	for _, schema := range exportMeasurementSchemas() {
		measurementCount, err := s.exportMeasurement(ctx, cutoff, schema, consume)
		count += measurementCount
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

func (s *MTSStore) ImportPoints(ctx context.Context, points []mts.Point) error {
	if len(points) == 0 {
		return nil
	}
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.engine.Write(ctx, points, mts.WriteOptions{Sync: true}); err != nil {
		return fmt.Errorf("import MTS points: %w", err)
	}
	return nil
}

type measurementSchema struct {
	name   string
	fields []string
}

func exportMeasurementSchemas() []measurementSchema {
	schemas := make([]measurementSchema, 0, len(model.MetricNames())+3)
	for _, name := range model.MetricNames() {
		schemas = append(schemas, measurementSchema{name: name, fields: []string{"value"}})
	}
	schemas = append(schemas,
		measurementSchema{name: trafficReceivedDelta, fields: []string{"value"}},
		measurementSchema{name: trafficSentDelta, fields: []string{"value"}},
		measurementSchema{name: networkProbeMeasurement,
			fields: []string{"latency_ms", "success", "status_code", "error_code"}},
	)
	return schemas
}

func (s *MTSStore) exportMeasurement(
	ctx context.Context,
	cutoff time.Time,
	schema measurementSchema,
	consume MTSRowConsumer,
) (int64, error) {
	query, err := mts.NewQuery().From("beat", "", schema.name).Select(schema.fields...).
		TimeRange(0, cutoff.UnixNano()).Precision(mts.PrecisionNanosecond).
		OrderByTimeAsc().Build()
	if err != nil {
		return 0, fmt.Errorf("build MTS export query for %s: %w", schema.name, err)
	}
	iterator, err := s.engine.QueryRowIterator(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("open MTS export iterator for %s: %w", schema.name, err)
	}
	var count int64
	for iterator.Next() {
		if err := consume(iterator.Row()); err != nil {
			closeErr := iterator.Close()
			return count, errors.Join(err, closeErr)
		}
		count++
	}
	iterationErr := iterator.Err()
	closeErr := iterator.Close()
	if err := errors.Join(iterationErr, closeErr); err != nil {
		return count, fmt.Errorf("iterate MTS export for %s: %w", schema.name, err)
	}
	return count, nil
}
