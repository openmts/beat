package model

import "fmt"

// MaxTransformationColumnBytes 是变换里列选择器的字节上限：filter_by_value 的 column 与 sort_by 的
// field 指向结果帧的一个表头（`field`、`calculation_max`、`group_node_id` 这类列键，或分组键本身），
// 64 字节足够覆盖真实列名，同时挡住把整段文档塞进列名的负载。
const MaxTransformationColumnBytes = 64

// validateTransformationColumn 校验列选择器：必须可打印且有界。空值由调用方按「未声明」处理。
func validateTransformationColumn(value string) error {
	if len(value) > MaxTransformationColumnBytes || !printableTransformationText(value) {
		return fmt.Errorf("%w: transformation column must be printable and at most %d bytes", ErrInvalidDashboard, MaxTransformationColumnBytes)
	}
	return nil
}
