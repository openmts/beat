package metriccatalog

import "github.com/beat/backend/internal/model"

// 匿名聚合遥测的固定标签集合；事件、页面种类与拒绝原因必须是已登记枚举，
// 任何自由文本、地址或访客标识都不得成为标签。
var (
	PublicInteractionTags         = []string{"event", "surface"}
	PublicInteractionRejectedTags = []string{"reason"}
)

var publicInteractionSeeds = []struct {
	key, label string
	tags       []string
}{
	{model.PublicInteractionTotalMetric, "Public interactions", PublicInteractionTags},
	{model.PublicInteractionRejectedMetric, "Rejected public interactions", PublicInteractionRejectedTags},
}

// PublicInteractions 返回匿名聚合遥测计数目录；它们从不对外暴露，只由管理端汇总读取。
func PublicInteractions() []model.MetricDefinition {
	result := make([]model.MetricDefinition, 0, len(publicInteractionSeeds))
	for _, seed := range publicInteractionSeeds {
		result = append(result, model.MetricDefinition{
			Key: seed.key, Label: seed.label, Source: "builtin", Kind: "counter", Unit: "count",
			Aggregations: []string{"min", "max", "last", "sum", "rate"}, DefaultAggregation: "sum",
			Public: false, Enabled: true, RetentionDays: DefaultRetentionDays, Revision: 1,
			Tags: seed.tags, Minimum: numberBound(0),
		})
	}
	return result
}
