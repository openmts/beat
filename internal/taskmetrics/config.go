package taskmetrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const DefaultMeasurement = "task_metrics"

var (
	measurementPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	keyPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	reserved           = map[string]struct{}{"node_metrics": {}, "network_probe": {}}
)

type Extractor struct {
	Type       string `json:"type"`
	Expression string `json:"expression"`
}

type Metric struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Extractor Extractor `json:"extractor"`
	Unit      string    `json:"unit"`
}

type Schema struct {
	Enabled     bool     `json:"enabled"`
	Measurement string   `json:"measurement"`
	Metrics     []Metric `json:"metrics"`
}

type Config struct {
	MetricSchema Schema `json:"metricSchema"`
}

func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, errors.New("指标编排配置格式无效")
	}
	normalized, err := NormalizeConfig(cfg)
	if err != nil {
		return Config{}, err
	}
	return normalized, nil
}

func NormalizeConfig(cfg Config) (Config, error) {
	cfg.MetricSchema.Measurement = strings.TrimSpace(cfg.MetricSchema.Measurement)
	if cfg.MetricSchema.Measurement == "" {
		cfg.MetricSchema.Measurement = DefaultMeasurement
	}
	if !measurementPattern.MatchString(cfg.MetricSchema.Measurement) {
		return Config{}, errors.New("MTS 表名称只能包含小写字母、数字和下划线，且必须以字母开头，最长 64 个字符")
	}
	if _, exists := reserved[cfg.MetricSchema.Measurement]; exists {
		return Config{}, fmt.Errorf("MTS 表名称 %q 为系统保留表，请使用其他名称", cfg.MetricSchema.Measurement)
	}
	if !cfg.MetricSchema.Enabled {
		cfg.MetricSchema.Metrics = normalizeMetrics(cfg.MetricSchema.Metrics)
		return cfg, nil
	}
	if len(cfg.MetricSchema.Metrics) == 0 {
		return Config{}, errors.New("开启指标采集后至少需要配置一个指标")
	}
	if len(cfg.MetricSchema.Metrics) > 64 {
		return Config{}, errors.New("单个任务最多配置 64 个指标")
	}
	seen := make(map[string]struct{}, len(cfg.MetricSchema.Metrics))
	metrics := make([]Metric, 0, len(cfg.MetricSchema.Metrics))
	for index, metric := range cfg.MetricSchema.Metrics {
		metric.Key = strings.TrimSpace(metric.Key)
		metric.Name = strings.TrimSpace(metric.Name)
		metric.Extractor.Type = strings.TrimSpace(metric.Extractor.Type)
		metric.Extractor.Expression = strings.TrimSpace(metric.Extractor.Expression)
		metric.Unit = strings.TrimSpace(metric.Unit)
		if !keyPattern.MatchString(metric.Key) {
			return Config{}, fmt.Errorf("第 %d 个指标的指标键无效，只能包含小写字母、数字和下划线，且必须以字母开头", index+1)
		}
		if _, exists := seen[metric.Key]; exists {
			return Config{}, fmt.Errorf("指标键 %q 重复", metric.Key)
		}
		seen[metric.Key] = struct{}{}
		if metric.Name == "" {
			metric.Name = metric.Key
		}
		if len([]rune(metric.Name)) > 64 {
			return Config{}, fmt.Errorf("指标 %q 的展示名称不能超过 64 个字符", metric.Key)
		}
		if len([]rune(metric.Unit)) > 16 {
			return Config{}, fmt.Errorf("指标 %q 的单位不能超过 16 个字符", metric.Key)
		}
		if metric.Extractor.Expression == "" {
			return Config{}, fmt.Errorf("指标 %q 的提取表达式不能为空", metric.Key)
		}
		if len(metric.Extractor.Expression) > 512 {
			return Config{}, fmt.Errorf("指标 %q 的提取表达式不能超过 512 个字符", metric.Key)
		}
		switch metric.Extractor.Type {
		case "json_path":
			if !strings.HasPrefix(metric.Extractor.Expression, "$") {
				return Config{}, fmt.Errorf("指标 %q 的 JSON 路径必须以 $ 开头", metric.Key)
			}
		case "key_value":
		case "regex":
			if _, err := regexp.Compile(metric.Extractor.Expression); err != nil {
				return Config{}, fmt.Errorf("指标 %q 的正则表达式无效: %v", metric.Key, err)
			}
		default:
			return Config{}, fmt.Errorf("指标 %q 使用了不支持的提取方式", metric.Key)
		}
		metrics = append(metrics, metric)
	}
	cfg.MetricSchema.Metrics = metrics
	return cfg, nil
}

func normalizeMetrics(metrics []Metric) []Metric {
	result := make([]Metric, 0, len(metrics))
	for _, metric := range metrics {
		metric.Key = strings.TrimSpace(metric.Key)
		metric.Name = strings.TrimSpace(metric.Name)
		metric.Extractor.Type = strings.TrimSpace(metric.Extractor.Type)
		metric.Extractor.Expression = strings.TrimSpace(metric.Extractor.Expression)
		metric.Unit = strings.TrimSpace(metric.Unit)
		if metric.Key == "" && metric.Extractor.Expression == "" {
			continue
		}
		result = append(result, metric)
	}
	return result
}

func MarshalConfig(cfg Config) (json.RawMessage, error) {
	normalized, err := NormalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode metric config: %w", err)
	}
	return data, nil
}
