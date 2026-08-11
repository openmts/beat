package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openmts/mts"
	_ "modernc.org/sqlite"

	"github.com/beat/backend/internal/model"
)

func validateSQLite(ctx context.Context, path string) error {
	database, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open restored SQLite snapshot: %w", err)
	}
	defer func() { _ = database.Close() }()
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("restored SQLite integrity check failed: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("restored SQLite integrity check failed: %s", integrity)
	}
	for _, table := range []string{"groups", "nodes", "admin_users", "admin_sessions", "admin_audit_events"} {
		var count int
		err := database.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count)
		if err != nil {
			return fmt.Errorf("inspect restored SQLite table %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("restored SQLite table %s is missing", table)
		}
	}
	return nil
}

func validateMetrics(path string, cutoff int64) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open MTS export: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return 0, fmt.Errorf("open compressed MTS export: %w", err)
	}
	defer func() { _ = reader.Close() }()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumJSONLLine)
	var count int64
	for scanner.Scan() {
		var record metricRecord
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil || !validMetricRecord(record, cutoff) {
			return count, fmt.Errorf("MTS export row %d is invalid", count+1)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return count, fmt.Errorf("MTS export row %d contains trailing data", count+1)
		}
		count++
		if count > maximumMetricRows {
			return count, errors.New("MTS export row count exceeds limit")
		}
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read MTS export: %w", err)
	}
	return count, nil
}

func validMetricRecord(record metricRecord, cutoff int64) bool {
	if record.TimestampNS <= 0 || record.TimestampNS > cutoff {
		return false
	}
	if record.Measurement == "network_probe" {
		return exactKeys(record.Tags, "task", "node", "type") &&
			exactFields(record.Fields, map[string]mts.FieldType{"latency_ms": mts.FieldFloat64,
				"success": mts.FieldFloat64, "status_code": mts.FieldInt64, "error_code": mts.FieldString})
	}
	if !knownMetric(record.Measurement) || !exactKeys(record.Tags, "node") {
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
		if values[key] == "" {
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
	}
	return true
}
