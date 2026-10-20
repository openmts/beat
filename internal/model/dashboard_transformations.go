package model

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// 变换种类沿用 Grafana 的 Transform 命名，便于看板在两侧互相理解。
var (
	dashboardTransformationKinds       = []string{"rename_by_regex", "filter_by_value", "filter_by_label", "sort_by", "limit", "binary_operation", "reduce", "group_by"}
	dashboardTransformationComparisons = []string{"gt", "gte", "lt", "lte", "eq", "ne"}
	dashboardTransformationOrders      = []string{"asc", "desc"}
	// 二元运算的运算符：四则运算，缺省不提供比较与逻辑运算符，避免一次改动同时定义两套语义。
	dashboardTransformationOperators = []string{"+", "-", "*", "/"}
	// group_by 可用的分组字段：与序列的身份字段一一对应。
	dashboardTransformationGroupFields = []string{"ref_id", "node_id", "metric", "aggregation"}
	// group_by 可用的聚合方式：range/delta/percent_change 需要同一条序列的首末样本，
	// 跨序列没有定义，因此不在这里出现。
	dashboardTransformationGroupCalculations = []string{CalculationSum, CalculationMean, CalculationMin, CalculationMax, CalculationCount}
	// reduce 的两种 Grafana 模式：reduce_fields 每条序列一个结果；series_to_rows 展开成长表行。
	dashboardTransformationReduceModes = []string{"reduce_fields", "series_to_rows"}
)

// validateDashboardTransformations 校验面板级变换链。每条变换只认自己那一组字段，其余字段必须
// 留空：混用的负载会被静默忽略，直接拒绝比让作者以为配置生效更安全。
func validateDashboardTransformations(panel DashboardPanel) error {
	if len(panel.Transformations) > MaxDashboardTransformations {
		return fmt.Errorf("%w: at most %d transformations are allowed", ErrInvalidDashboard, MaxDashboardTransformations)
	}
	for _, transformation := range panel.Transformations {
		if !slices.Contains(dashboardTransformationKinds, transformation.Kind) {
			return fmt.Errorf("%w: transformation kind is invalid", ErrInvalidDashboard)
		}
		if err := validateDashboardTransformation(transformation); err != nil {
			return err
		}
	}
	return nil
}

func validateDashboardTransformation(transformation DashboardTransformation) error {
	switch transformation.Kind {
	case "rename_by_regex":
		return validateDashboardRenameTransformation(transformation)
	case "filter_by_value":
		return validateDashboardFilterTransformation(transformation)
	case "filter_by_label":
		return validateDashboardLabelFilterTransformation(transformation)
	case "sort_by":
		return validateDashboardSortTransformation(transformation)
	case "binary_operation":
		return validateDashboardBinaryOperationTransformation(transformation)
	case "reduce":
		return validateDashboardReduceTransformation(transformation)
	case "group_by":
		return validateDashboardGroupByTransformation(transformation)
	default:
		return validateDashboardLimitTransformation(transformation)
	}
}

func validateDashboardRenameTransformation(transformation DashboardTransformation) error {
	if transformation.Pattern == "" || len(transformation.Pattern) > MaxTransformationPatternBytes {
		return fmt.Errorf("%w: rename requires a pattern of at most %d bytes", ErrInvalidDashboard, MaxTransformationPatternBytes)
	}
	if _, err := regexp.Compile(transformation.Pattern); err != nil {
		return fmt.Errorf("%w: rename pattern must be a valid expression", ErrInvalidDashboard)
	}
	if len(transformation.Replacement) > MaxTransformationReplacementBytes || !printableTransformationText(transformation.Replacement) {
		return fmt.Errorf("%w: rename replacement must be printable and at most %d bytes", ErrInvalidDashboard, MaxTransformationReplacementBytes)
	}
	if transformation.Comparison != "" || transformation.Value != nil || transformation.Statistic != "" ||
		transformation.Order != "" || transformation.Count != nil || transformation.Invert ||
		transformation.Mode != "" || len(transformation.Calculations) > 0 ||
		transformation.Column != "" || transformation.Field != "" {
		return fmt.Errorf("%w: rename accepts only pattern and replacement", ErrInvalidDashboard)
	}
	return nil
}

func validateDashboardFilterTransformation(transformation DashboardTransformation) error {
	if !slices.Contains(dashboardTransformationComparisons, transformation.Comparison) {
		return fmt.Errorf("%w: filter comparison is invalid", ErrInvalidDashboard)
	}
	if transformation.Value == nil || !isFinite(*transformation.Value) {
		return fmt.Errorf("%w: filter requires a finite comparison value", ErrInvalidDashboard)
	}
	if transformation.Column != "" {
		if err := validateTransformationColumn(transformation.Column); err != nil {
			return err
		}
	}
	if transformation.Pattern != "" || transformation.Replacement != "" || transformation.Statistic != "" ||
		transformation.Order != "" || transformation.Count != nil || transformation.Invert ||
		transformation.Mode != "" || len(transformation.Calculations) > 0 || transformation.Field != "" {
		return fmt.Errorf("%w: filter accepts only comparison, value and column", ErrInvalidDashboard)
	}
	return nil
}

// filter_by_label 只看序列标签：命中正则的序列保留，Invert 时反过来保留未命中的序列。
func validateDashboardLabelFilterTransformation(transformation DashboardTransformation) error {
	if transformation.Pattern == "" || len(transformation.Pattern) > MaxTransformationPatternBytes {
		return fmt.Errorf("%w: label filter requires a pattern of at most %d bytes", ErrInvalidDashboard, MaxTransformationPatternBytes)
	}
	if _, err := regexp.Compile(transformation.Pattern); err != nil {
		return fmt.Errorf("%w: label filter pattern must be a valid expression", ErrInvalidDashboard)
	}
	if transformation.Replacement != "" || transformation.Comparison != "" || transformation.Value != nil ||
		transformation.Statistic != "" || transformation.Order != "" || transformation.Count != nil ||
		transformation.Mode != "" || len(transformation.Calculations) > 0 ||
		transformation.Column != "" || transformation.Field != "" {
		return fmt.Errorf("%w: label filter accepts only pattern and invert", ErrInvalidDashboard)
	}
	return nil
}

func validateDashboardSortTransformation(transformation DashboardTransformation) error {
	if transformation.Statistic != "" && !slices.Contains(dashboardLegendValues, transformation.Statistic) {
		return fmt.Errorf("%w: sort statistic is invalid", ErrInvalidDashboard)
	}
	if transformation.Statistic == "" && transformation.Field == "" {
		return fmt.Errorf("%w: sort needs a statistic or a field", ErrInvalidDashboard)
	}
	if transformation.Field != "" {
		if err := validateTransformationColumn(transformation.Field); err != nil {
			return err
		}
	}
	if !slices.Contains(dashboardTransformationOrders, transformation.Order) {
		return fmt.Errorf("%w: sort order is invalid", ErrInvalidDashboard)
	}
	if transformation.Pattern != "" || transformation.Replacement != "" || transformation.Comparison != "" ||
		transformation.Value != nil || transformation.Count != nil || transformation.Invert ||
		transformation.Mode != "" || len(transformation.Calculations) > 0 || transformation.Column != "" {
		return fmt.Errorf("%w: sort accepts only statistic, field and order", ErrInvalidDashboard)
	}
	return nil
}

// limit 只裁剪序列条数，不做排序；需要「最大的三条」时把 sort_by 放在 limit 前面。
func validateDashboardLimitTransformation(transformation DashboardTransformation) error {
	if transformation.Count == nil || *transformation.Count < 1 || *transformation.Count > MaxDashboardSeries {
		return fmt.Errorf("%w: limit must keep between 1 and %d series", ErrInvalidDashboard, MaxDashboardSeries)
	}
	if transformation.Pattern != "" || transformation.Replacement != "" || transformation.Comparison != "" ||
		transformation.Value != nil || transformation.Statistic != "" || transformation.Order != "" || transformation.Invert ||
		transformation.Mode != "" || len(transformation.Calculations) > 0 ||
		transformation.Column != "" || transformation.Field != "" {
		return fmt.Errorf("%w: limit accepts only count", ErrInvalidDashboard)
	}
	return nil
}

// 二元运算按时间戳对齐左右两条序列；选择器是标签精确匹配，写成 /re/ 时按 RE2 正则取第一条命中。
func validateDashboardBinaryOperationTransformation(transformation DashboardTransformation) error {
	if err := validateTransformationSelector(transformation.Left); err != nil {
		return fmt.Errorf("%w: binary operation left selector is invalid", ErrInvalidDashboard)
	}
	if err := validateTransformationSelector(transformation.Right); err != nil {
		return fmt.Errorf("%w: binary operation right selector is invalid", ErrInvalidDashboard)
	}
	if !slices.Contains(dashboardTransformationOperators, transformation.Operator) {
		return fmt.Errorf("%w: binary operation operator must be +, -, * or /", ErrInvalidDashboard)
	}
	if len(transformation.Alias) > MaxTransformationReplacementBytes || !printableTransformationText(transformation.Alias) {
		return fmt.Errorf("%w: binary operation alias must be printable and at most %d bytes", ErrInvalidDashboard, MaxTransformationReplacementBytes)
	}
	if transformation.Pattern != "" || transformation.Replacement != "" || transformation.Comparison != "" ||
		transformation.Value != nil || transformation.Statistic != "" || transformation.Order != "" ||
		transformation.Count != nil || transformation.Invert ||
		transformation.Mode != "" || len(transformation.Calculations) > 0 ||
		transformation.Column != "" || transformation.Field != "" {
		return fmt.Errorf("%w: binary operation accepts only left, operator, right and alias", ErrInvalidDashboard)
	}
	return nil
}

// reduce 把每条序列按窗口压成结果，取值方式与面板级 Calculation 共用一份集合：这样
// 「stat 面板的计算」与「Reduce 变换」不会各自定义一套语义。历史看板用单个 calculation；
// 新看板可用 calculations 多选，但只有 series_to_rows 能把多个计算方式各自摊成一行。
func validateDashboardReduceTransformation(transformation DashboardTransformation) error {
	calculations, err := transformationCalculationList(transformation.Calculation, transformation.Calculations, dashboardCalculations)
	if err != nil {
		return err
	}
	if transformation.Mode != "" && !slices.Contains(dashboardTransformationReduceModes, transformation.Mode) {
		return fmt.Errorf("%w: reduce mode must be reduce_fields or series_to_rows", ErrInvalidDashboard)
	}
	if len(calculations) > 1 && transformation.Mode != "series_to_rows" {
		return fmt.Errorf("%w: reduce with multiple calculations requires mode series_to_rows", ErrInvalidDashboard)
	}
	if transformation.Pattern != "" || transformation.Replacement != "" || transformation.Comparison != "" ||
		transformation.Value != nil || transformation.Statistic != "" || transformation.Order != "" ||
		transformation.Count != nil || transformation.Invert || transformation.Left != "" ||
		transformation.Operator != "" || transformation.Right != "" || transformation.Alias != "" ||
		transformation.GroupBy != "" || transformation.Column != "" || transformation.Field != "" {
		return fmt.Errorf("%w: reduce accepts only calculation, calculations and mode", ErrInvalidDashboard)
	}
	return nil
}

// group_by 把同一字段取值的多条序列合并成一条：每个时间点对组内有效样本做聚合，
// 用于把「每个节点一条线」压成「每个指标一条线」这类看板。
//
// 与 Grafana 的 Group by 一样可以选多个计算方式：每个计算方式各生成一条合并序列，标签由
// Alias（缺省用分组键）加上计算方式后缀组成，避免「选了多个但只有一个生效」。
func validateDashboardGroupByTransformation(transformation DashboardTransformation) error {
	if !slices.Contains(dashboardTransformationGroupFields, transformation.GroupBy) {
		return fmt.Errorf("%w: group by field must be ref_id, node_id, metric or aggregation", ErrInvalidDashboard)
	}
	if _, err := transformationCalculationList(transformation.Calculation, transformation.Calculations, dashboardTransformationGroupCalculations); err != nil {
		return err
	}
	if len(transformation.Alias) > MaxTransformationReplacementBytes || !printableTransformationText(transformation.Alias) {
		return fmt.Errorf("%w: group by alias must be printable and at most %d bytes", ErrInvalidDashboard, MaxTransformationReplacementBytes)
	}
	if transformation.Pattern != "" || transformation.Replacement != "" || transformation.Comparison != "" ||
		transformation.Value != nil || transformation.Statistic != "" || transformation.Order != "" ||
		transformation.Count != nil || transformation.Invert || transformation.Left != "" ||
		transformation.Operator != "" || transformation.Right != "" || transformation.Mode != "" ||
		transformation.Column != "" || transformation.Field != "" {
		return fmt.Errorf("%w: group by accepts only group_by, calculation, calculations and alias", ErrInvalidDashboard)
	}
	return nil
}

// transformationCalculationList 统一「单个 calculation」与「calculations 多选」两种写法：同时出现、
// 一个都没有、出现不属于该种类的值、重复值或超过上限都返回 ErrInvalidDashboard，避免负载里写着
// 多个计算方式而渲染器只消费其中一个。
func transformationCalculationList(single string, multiple []string, allowed []string) ([]string, error) {
	if single != "" && len(multiple) > 0 {
		return nil, fmt.Errorf("%w: calculation and calculations are mutually exclusive", ErrInvalidDashboard)
	}
	values := multiple
	if single != "" {
		values = []string{single}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: at least one calculation is required", ErrInvalidDashboard)
	}
	if len(values) > MaxDashboardTransformationCalculations {
		return nil, fmt.Errorf("%w: at most %d calculations are allowed", ErrInvalidDashboard, MaxDashboardTransformationCalculations)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return nil, fmt.Errorf("%w: calculation is not allowed for this transformation", ErrInvalidDashboard)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("%w: calculation is duplicated", ErrInvalidDashboard)
		}
		seen[value] = struct{}{}
	}
	return values, nil
}

// validateTransformationSelector 接受标签精确文本或 /re/ 形式的正则，长度与可打印性同其它变换字段。
func validateTransformationSelector(selector string) error {
	if selector == "" || len(selector) > MaxTransformationPatternBytes || !printableTransformationText(selector) {
		return ErrInvalidDashboard
	}
	pattern, ok := transformationSelectorPattern(selector)
	if !ok {
		return nil
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return ErrInvalidDashboard
	}
	return nil
}

// transformationSelectorPattern 只在选择器两端都是斜杠且长度足够时按正则解释；否则整串都是字面量。
func transformationSelectorPattern(selector string) (string, bool) {
	if len(selector) < 3 || !strings.HasPrefix(selector, "/") || !strings.HasSuffix(selector, "/") {
		return "", false
	}
	pattern := selector[1 : len(selector)-1]
	if pattern == "" {
		return "", false
	}
	return pattern, true
}

// 标签最终会进入 HTML 与 CSV 导出，替换文本只允许可打印字符，控制字符一律拒绝。
func printableTransformationText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
