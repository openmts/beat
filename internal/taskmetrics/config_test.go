package taskmetrics

import (
	"encoding/json"
	"testing"
)

func TestParseConfigNormalizesAndValidates(t *testing.T) {
	cfg, err := ParseConfig(json.RawMessage(`{"metricSchema":{"enabled":true,"measurement":"app_metrics","metrics":[{"key":"qps","name":"QPS","extractor":{"type":"json_path","expression":"$.qps"},"unit":"次/秒"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricSchema.Measurement != "app_metrics" || len(cfg.MetricSchema.Metrics) != 1 || cfg.MetricSchema.Metrics[0].Key != "qps" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseConfigRejectsReservedMeasurement(t *testing.T) {
	_, err := ParseConfig(json.RawMessage(`{"metricSchema":{"enabled":true,"measurement":"node_metrics","metrics":[{"key":"qps","extractor":{"type":"json_path","expression":"$.qps"}}]}}`))
	if err == nil {
		t.Fatal("expected reserved measurement to be rejected")
	}
}

func TestExtractJSONPathKeyValueAndRegex(t *testing.T) {
	cfg, err := ParseConfig(json.RawMessage(`{"metricSchema":{"enabled":true,"measurement":"app_metrics","metrics":[{"key":"qps","extractor":{"type":"json_path","expression":"$.pool.qps"}},{"key":"connections","extractor":{"type":"json_path","expression":"$.connections"}},{"key":"sessions","extractor":{"type":"key_value","expression":"sessions"}},{"key":"lag","extractor":{"type":"regex","expression":"lag_ms:\\s*([0-9.]+)"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	values, err := Extract(cfg, "{\"pool\":{\"qps\":118.7},\"connections\":42}\nsessions=7\nlag_ms: 0.4\n")
	if err != nil {
		t.Fatal(err)
	}
	if values["qps"] != 118.7 || values["connections"] != 42 || values["sessions"] != 7 || values["lag"] != 0.4 {
		t.Fatalf("unexpected values: %+v", values)
	}
}

func TestExtractSkipsMissingAndNonNumericValues(t *testing.T) {
	cfg, err := ParseConfig(json.RawMessage(`{"metricSchema":{"enabled":true,"measurement":"app_metrics","metrics":[{"key":"missing","extractor":{"type":"json_path","expression":"$.missing"}},{"key":"text","extractor":{"type":"json_path","expression":"$.text"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	values, err := Extract(cfg, `{"text":"not-a-number"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("expected no values, got %+v", values)
	}
}
