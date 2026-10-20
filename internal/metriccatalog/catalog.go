package metriccatalog

import (
	"errors"
	"strings"
	"sync"

	"github.com/beat/backend/internal/model"
)

const (
	Version                 = 1
	MaximumDefinitions      = 4096
	MaximumNameBytes        = 100
	MaximumLabelBytes       = 128
	MaximumDescriptionBytes = 2048
	MinimumRetentionDays    = 1
	MaximumRetentionDays    = 3650
	DefaultRetentionDays    = 30
)

var ErrInvalidDefinition = errors.New("invalid metric definition")

type metricSeed struct{ key, label, unit string }

var builtinSeeds = []metricSeed{
	{"cpu", "CPU usage", "percent"}, {"cpu_used", "CPU used", "cores"}, {"cpu_total", "CPU capacity", "cores"},
	{"memory", "Memory usage", "percent"}, {"memory_used", "Memory used", "bytes"}, {"memory_total", "Memory capacity", "bytes"},
	{"disk", "Disk usage", "percent"}, {"disk_used", "Disk used", "bytes"}, {"disk_total", "Disk capacity", "bytes"},
	{"disk_read", "Disk read", "bytes_per_second"}, {"disk_write", "Disk write", "bytes_per_second"},
	{"net_recv", "Network received", "bytes_per_second"}, {"net_sent", "Network sent", "bytes_per_second"},
	{"net_recv_total", "Network received total", "bytes"}, {"net_sent_total", "Network sent total", "bytes"},
	{"swap", "Swap usage", "percent"}, {"swap_used", "Swap used", "bytes"}, {"swap_total", "Swap capacity", "bytes"},
	{"load1", "Load average (1 minute)", "number"}, {"load5", "Load average (5 minutes)", "number"}, {"load15", "Load average (15 minutes)", "number"},
	{"uptime", "Uptime", "seconds"}, {"processes", "Processes", "count"},
	{"tcp_connections", "TCP connections", "count"}, {"udp_connections", "UDP connections", "count"},
	{"net_recv_delta", "Received traffic", "bytes"}, {"net_sent_delta", "Sent traffic", "bytes"},
}

func Builtins() []model.MetricDefinition {
	definitions := make([]model.MetricDefinition, 0, len(builtinSeeds))
	for _, seed := range builtinSeeds {
		definitions = append(definitions, builtinDefinition(seed))
	}
	definitions = append(definitions, Accelerators()...)
	definitions = append(definitions, Diagnostics()...)
	return append(definitions, PublicInteractions()...)
}

func builtinDefinition(seed metricSeed) model.MetricDefinition {
	definition := model.MetricDefinition{Key: seed.key, Label: seed.label, Source: "builtin", Kind: "gauge", Unit: seed.unit,
		Aggregations: []string{"avg", "min", "max", "last"}, DefaultAggregation: "avg", Public: true, Enabled: true,
		RetentionDays: DefaultRetentionDays, Revision: 1, Tags: []string{"node"}, Minimum: numberBound(0)}
	if seed.unit == "percent" {
		definition.Maximum = numberBound(100)
	}
	switch seed.key {
	case "net_recv_total", "net_sent_total":
		definition.Kind, definition.DefaultAggregation = "counter", "last"
		definition.Aggregations = []string{"min", "max", "last", "rate"}
	case "net_recv_delta", "net_sent_delta":
		definition.Source, definition.DefaultAggregation = "derived", "sum"
		definition.Aggregations = []string{"sum", "min", "max", "last"}
	}
	return definition
}

func Custom(name, description string) (model.MetricDefinition, error) {
	if !validName(name) {
		return model.MetricDefinition{}, ErrInvalidDefinition
	}
	definition := model.MetricDefinition{Key: model.CustomMetricPrefix + name, Label: name, Description: description,
		Source: "custom", Kind: "gauge", Unit: "number", Aggregations: []string{"avg", "min", "max", "sum", "last"},
		DefaultAggregation: "avg", Public: true, Enabled: true, RetentionDays: DefaultRetentionDays, Revision: 1, Tags: []string{"node"}}
	if err := ValidateDefinition(definition); err != nil {
		return model.MetricDefinition{}, err
	}
	return definition, nil
}

func validName(name string) bool {
	if name == "" || len(name) > MaximumNameBytes {
		return false
	}
	for _, character := range name {
		if !validCharacter(character) {
			return false
		}
	}
	return true
}

func validCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_' || character == '.' || character == '-'
}

func ValidKey(key string) bool {
	if strings.HasPrefix(key, model.CustomMetricPrefix) {
		return validName(strings.TrimPrefix(key, model.CustomMetricPrefix))
	}
	return validName(key)
}

func numberBound(value float64) *float64 { return &value }

// defaultAggregations 只在首次调用时构建一次内置指标的默认聚合索引。
var defaultAggregations = sync.OnceValue(func() map[string]string {
	index := make(map[string]string, len(builtinSeeds))
	for _, definition := range Builtins() {
		index[definition.Key] = definition.DefaultAggregation
	}
	return index
})

// DefaultAggregationForKey 返回内置指标的默认聚合；未登记的自定义指标按 gauge 处理。
func DefaultAggregationForKey(key string) string {
	if aggregation, ok := defaultAggregations()[key]; ok {
		return aggregation
	}
	return "avg"
}
