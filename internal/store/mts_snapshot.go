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

type MetricExport struct {
	Cutoff      time.Time
	Definitions []model.MetricDefinition
}

func (s *MTSStore) ExportRows(
	ctx context.Context,
	cutoff time.Time,
	consume MTSRowConsumer,
) (int64, error) {
	return s.ExportCatalogRows(ctx, MetricExport{Cutoff: cutoff}, consume)
}

func (s *MTSStore) ExportCatalogRows(ctx context.Context, request MetricExport, consume MTSRowConsumer) (int64, error) {
	snapshot, err := s.BeginMetricSnapshot(ctx)
	if err != nil {
		return 0, err
	}
	defer snapshot.Close()
	return snapshot.ExportCatalogRows(ctx, request, consume)
}

func (s *MTSStore) exportCatalogRowsLocked(ctx context.Context, request MetricExport, consume MTSRowConsumer) (int64, error) {
	if consume == nil {
		return 0, errors.New("MTS row consumer is required")
	}
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}
	if err := s.engine.Flush(ctx); err != nil {
		return 0, fmt.Errorf("flush MTS before export: %w", err)
	}
	if request.Definitions == nil && s.catalog != nil {
		catalog, err := s.catalog.Catalog(ctx, false)
		if err != nil {
			return 0, err
		}
		request.Definitions = catalog.Metrics
	}
	var count int64
	for _, schema := range exportCatalogSchemas(request.Definitions) {
		measurementCount, err := s.exportMeasurement(ctx, request.Cutoff, schema, consume)
		count += measurementCount
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

func exportCatalogSchemas(definitions []model.MetricDefinition) []measurementSchema {
	if definitions == nil {
		return exportMeasurementSchemas()
	}
	schemas := make([]measurementSchema, 0, len(definitions)+1)
	hasAccelerators := false
	for _, definition := range definitions {
		schemas = append(schemas, measurementSchema{name: definition.Key, fields: []string{"value"}})
		if model.ValidAcceleratorMetric(definition.Key) {
			hasAccelerators = true
		}
	}
	if hasAccelerators {
		schemas = append(schemas, measurementSchema{name: model.AcceleratorSampleMeasurement, fields: acceleratorSampleFieldNames()})
	}
	return append(schemas, measurementSchema{name: networkProbeMeasurement, fields: []string{"latency_ms", "success", "status_code", "error_code"}},
		measurementSchema{name: model.AgentReceiptMeasurement, fields: agentReceiptFieldNames()})
}

func (s *MTSStore) ImportPoints(ctx context.Context, points []mts.Point) error {
	if len(points) == 0 {
		return nil
	}
	s.operationMu.RLock()
	defer s.operationMu.RUnlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
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
	for _, name := range model.AcceleratorMetricKeys {
		schemas = append(schemas, measurementSchema{name: name, fields: []string{"value"}})
	}
	schemas = append(schemas,
		measurementSchema{name: trafficReceivedDelta, fields: []string{"value"}},
		measurementSchema{name: trafficSentDelta, fields: []string{"value"}},
		measurementSchema{name: networkProbeMeasurement,
			fields: []string{"latency_ms", "success", "status_code", "error_code"}},
		measurementSchema{name: model.AgentReceiptMeasurement, fields: agentReceiptFieldNames()},
		measurementSchema{name: model.AcceleratorSampleMeasurement, fields: acceleratorSampleFieldNames()},
	)
	for _, name := range model.PublicInteractionMetrics() {
		schemas = append(schemas, measurementSchema{name: name, fields: []string{model.PublicInteractionSampleField}})
	}
	return schemas
}

// exportSampleBudget 返回 MTS 导出的读取预算；测试可以注入更小的值来验证预算真的生效。
func (s *MTSStore) exportSampleBudget() int64 {
	if s.exportBudgetOverride > 0 {
		return s.exportBudgetOverride
	}
	return model.MaximumBackupMetricRows
}

// exportQuery 构造全量导出查询。导出是全量快照，因此显式预算必须覆盖归档允许的全部行，
// 否则引擎默认的 100 万样本读取保护会让数据量偏大的部署永远生不成备份。预算保持正数，
// 超过归档上限时仍然返回可识别的读取预算错误，而不是静默截断。
func (s *MTSStore) exportQuery(schema measurementSchema, cutoff time.Time) (mts.Query, error) {
	query, err := mts.NewQuery().From("beat", "", schema.name).Select(schema.fields...).
		TimeRange(0, cutoff.UnixNano()).Precision(mts.PrecisionNanosecond).
		OrderByTimeAsc().Build()
	if err != nil {
		return query, err
	}
	query.Budget = mts.QueryBudget{MaxSamples: int(s.exportSampleBudget())}
	return query, nil
}

func (s *MTSStore) exportMeasurement(
	ctx context.Context,
	cutoff time.Time,
	schema measurementSchema,
	consume MTSRowConsumer,
) (int64, error) {
	query, err := s.exportQuery(schema, cutoff)
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
