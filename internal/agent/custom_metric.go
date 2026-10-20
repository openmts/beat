package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// maxCustomMetricOutputToken 限定回显的非法输出长度，避免把整段输出写进日志。
const maxCustomMetricOutputToken = 32

var (
	errMetricValue      = errors.New("custom metric value must be a finite number")
	errMetricEmpty      = errors.New("custom metric command printed no value")
	errMetricNotANumber = errors.New("custom metric command must print a single number")
)

func CollectMetric(ctx context.Context, metric registeredMetric) (float64, bool) {
	value, err := executeCustomMetric(ctx, metric)
	return value, err == nil
}

// parseMetricValue 只接受输出的第一个字段，并区分「没有输出」「不是数字」「不是有限值」
// 三种失败，避免运维把命令格式问题误读成采集器故障。
func parseMetricValue(output string) (float64, error) {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) == 0 {
		return 0, errMetricEmpty
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", errMetricNotANumber, truncateMetricToken(fields[0]))
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errMetricValue
	}
	return value, nil
}

func truncateMetricToken(token string) string {
	runes := []rune(token)
	if len(runes) <= maxCustomMetricOutputToken {
		return token
	}
	return string(runes[:maxCustomMetricOutputToken])
}
