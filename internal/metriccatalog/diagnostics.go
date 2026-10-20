package metriccatalog

import (
	"math"
	"slices"

	"github.com/beat/backend/internal/model"
)

// DiagnosticMTRTags 与 DiagnosticIperfTags 是诊断测量的固定标签集合；任何
// 主机名、IP、目标名、作业错误文本都不得成为标签。
var (
	DiagnosticMTRTags   = []string{"node", "job", "hop", "stat"}
	DiagnosticIperfTags = []string{"node", "job", "direction", "protocol", "role"}
)

type diagnosticSeed struct {
	key, label, unit string
	aggregation      string
	maximum          float64
}

var diagnosticMTRSeeds = []diagnosticSeed{
	{model.DiagnosticMetricMTRLatencyMS, "MTR hop latency", "milliseconds", "avg", 10 * 60 * 1000},
	{model.DiagnosticMetricMTRLossPercent, "MTR hop loss", "percent", "last", 100},
	{model.DiagnosticMetricMTRSentPackets, "MTR hop packets sent", "count", "last", 100000},
	{model.DiagnosticMetricMTRReceivedPackets, "MTR hop packets received", "count", "last", 100000},
}

var diagnosticIperfSeeds = []diagnosticSeed{
	{model.DiagnosticMetricIperfBytesPerSecond, "iPerf3 throughput", "bytes_per_second", "avg", 1 << 40},
	{model.DiagnosticMetricIperfBytes, "iPerf3 transferred bytes", "bytes", "sum", 1 << 44},
	{model.DiagnosticMetricIperfRetransmits, "iPerf3 retransmits", "count", "last", 1000000},
	{model.DiagnosticMetricIperfJitterMS, "iPerf3 jitter", "milliseconds", "avg", 10 * 60 * 1000},
	{model.DiagnosticMetricIperfLossPercent, "iPerf3 loss", "percent", "last", 100},
}

// Diagnostics 返回诊断测量目录；它们从不公开，只能由管理员诊断结果接口读取。
func Diagnostics() []model.MetricDefinition {
	result := make([]model.MetricDefinition, 0, len(diagnosticMTRSeeds)+len(diagnosticIperfSeeds))
	for _, seed := range diagnosticMTRSeeds {
		result = append(result, diagnosticDefinition(seed, DiagnosticMTRTags))
	}
	for _, seed := range diagnosticIperfSeeds {
		result = append(result, diagnosticDefinition(seed, DiagnosticIperfTags))
	}
	return result
}

func diagnosticDefinition(seed diagnosticSeed, tags []string) model.MetricDefinition {
	aggregation := seed.aggregation
	definition := model.MetricDefinition{Key: seed.key, Label: seed.label, Source: "builtin", Kind: "gauge",
		Unit: seed.unit, Aggregations: []string{"avg", "min", "max", "sum", "last"}, DefaultAggregation: aggregation,
		Public: false, Enabled: true, RetentionDays: DefaultRetentionDays, Revision: 1,
		Tags: tags, Minimum: numberBound(0), Maximum: numberBound(seed.maximum)}
	if seed.unit == "bytes" {
		definition.Kind = "counter"
	}
	if seed.unit == "percent" {
		definition.Aggregations = []string{"avg", "min", "max", "last"}
	}
	if math.IsNaN(seed.maximum) {
		definition.Maximum = nil
	}
	return definition
}

func validMetricTags(definition model.MetricDefinition) bool {
	switch {
	case model.ValidAcceleratorMetric(definition.Key):
		return definition.Source == "builtin" && slices.Equal(definition.Tags, []string{"node", "device", "vendor"})
	case model.IsMTRDiagnosticMetric(definition.Key):
		return definition.Source == "builtin" && slices.Equal(definition.Tags, DiagnosticMTRTags)
	case model.IsIperfDiagnosticMetric(definition.Key):
		return definition.Source == "builtin" && slices.Equal(definition.Tags, DiagnosticIperfTags)
	case definition.Key == model.PublicInteractionTotalMetric:
		return definition.Source == "builtin" && slices.Equal(definition.Tags, PublicInteractionTags)
	case definition.Key == model.PublicInteractionRejectedMetric:
		return definition.Source == "builtin" && slices.Equal(definition.Tags, PublicInteractionRejectedTags)
	default:
		return slices.Equal(definition.Tags, []string{"node"})
	}
}
