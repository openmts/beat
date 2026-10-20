package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/openmts/mts"
	_ "modernc.org/sqlite"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func validateMetrics(path string, cutoff int64) (int64, error) {
	return validateCatalogMetrics(path, cutoff, nil)
}

func validateCatalogMetrics(path string, cutoff int64, definitions map[string]model.MetricDefinition) (int64, error) {
	return validateCatalogMetricsContext(context.Background(), metricValidationInput{path: path, cutoff: cutoff, definitions: definitions})
}

type metricValidationInput struct {
	path        string
	cutoff      int64
	definitions map[string]model.MetricDefinition
}

func validateCatalogMetricsContext(ctx context.Context, input metricValidationInput) (count int64, resultErr error) {
	file, err := os.Open(input.path)
	if err != nil {
		return 0, fmt.Errorf("open MTS export: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return 0, fmt.Errorf("open compressed MTS export: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, reader.Close()) }()
	memory := &acceleratorArchiveMemory{parent: filepath.Dir(input.path)}
	defer func() { resultErr = errors.Join(resultErr, memory.close()) }()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumJSONLLine)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		record, err := decodeArchiveMetric(scanner.Text(), input)
		if err != nil {
			return count, fmt.Errorf("MTS export row %d is invalid: %w", count+1, err)
		}
		if err := memory.add(ctx, record); err != nil {
			return count, err
		}
		count++
		if count > maximumMetricRows {
			return count, errors.New("MTS export row count exceeds limit")
		}
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read MTS export: %w", err)
	}
	return count, memory.finish(ctx)
}

func decodeArchiveMetric(line string, input metricValidationInput) (metricRecord, error) {
	var record metricRecord
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, err
	}
	if !validCatalogMetricRecord(record, input.cutoff, input.definitions) {
		return record, errors.New("invalid metric schema or value")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return record, errors.New("trailing metric data")
	}
	return record, nil
}

func validMetricRecord(record metricRecord, cutoff int64) bool {
	return validCatalogMetricRecord(record, cutoff, nil)
}

func validCatalogMetricRecord(record metricRecord, cutoff int64, definitions map[string]model.MetricDefinition) bool {
	if record.TimestampNS <= 0 || record.TimestampNS > cutoff {
		return false
	}
	if model.ValidAcceleratorMetric(record.Measurement) || record.Measurement == model.AcceleratorSampleMeasurement {
		return validAcceleratorArchiveRecord(record, definitions)
	}
	if record.Measurement == model.AgentReceiptMeasurement {
		if !exactKeys(record.Tags, "node") {
			return false
		}
		_, err := store.DecodeAgentReceipt(mts.Row{Tags: record.Tags, Fields: record.Fields, Timestamp: record.TimestampNS})
		return err == nil
	}
	if record.Measurement == "network_probe" {
		return exactKeys(record.Tags, "task", "node", "type") &&
			exactFields(record.Fields, map[string]mts.FieldType{"latency_ms": mts.FieldFloat64,
				"success": mts.FieldFloat64, "status_code": mts.FieldInt64, "error_code": mts.FieldString})
	}
	if !exactKeys(record.Tags, "node") {
		return false
	}
	return validScalarMetricRecord(record, definitions)
}

func validScalarMetricRecord(record metricRecord, definitions map[string]model.MetricDefinition) bool {
	if definitions != nil {
		definition, ok := definitions[record.Measurement]
		if !ok {
			return false
		}
		value := record.Fields["value"].Float64
		if definition.Minimum != nil && value < *definition.Minimum {
			return false
		}
		if definition.Maximum != nil && value > *definition.Maximum {
			return false
		}
	} else if !knownMetric(record.Measurement) {
		return false
	}
	return exactFields(record.Fields, map[string]mts.FieldType{"value": mts.FieldFloat64})
}

func knownMetric(name string) bool {
	if name == "net_recv_delta" || name == "net_sent_delta" {
		return true
	}
	for _, candidate := range model.MetricNames() {
		if candidate == name {
			return true
		}
	}
	return false
}

func exactKeys(values map[string]string, keys ...string) bool {
	if len(values) != len(keys) {
		return false
	}
	for _, key := range keys {
		if values[key] == "" || len(values[key]) > 128 {
			return false
		}
	}
	return true
}

func exactFields(values map[string]mts.FieldValue, expected map[string]mts.FieldType) bool {
	if len(values) != len(expected) {
		return false
	}
	for name, fieldType := range expected {
		if values[name].Type != fieldType {
			return false
		}
		if fieldType == mts.FieldFloat64 && (math.IsNaN(values[name].Float64) || math.IsInf(values[name].Float64, 0)) {
			return false
		}
	}
	return true
}
