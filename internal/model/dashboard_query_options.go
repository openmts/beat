package model

import "fmt"

// 查询预算与 internal/metricquery.ValidateRequest 保持一致：每条序列 2..600 个点。
// now delay 只回退相对时间的 now，绝对范围不受影响。
const (
	MinDashboardQueryPoints     = 2
	MaxDashboardQueryPoints     = 600
	MaxDashboardNowDelaySeconds = 3600
)

func validateDashboardPoints(value *int) error {
	if value == nil {
		return nil
	}
	if *value < MinDashboardQueryPoints || *value > MaxDashboardQueryPoints {
		return fmt.Errorf("%w: max data points must be between %d and %d", ErrInvalidDashboard, MinDashboardQueryPoints, MaxDashboardQueryPoints)
	}
	return nil
}

func validateDashboardNowDelay(value *int) error {
	if value == nil {
		return nil
	}
	if *value < 0 || *value > MaxDashboardNowDelaySeconds {
		return fmt.Errorf("%w: now delay must be between 0 and %d seconds", ErrInvalidDashboard, MaxDashboardNowDelaySeconds)
	}
	return nil
}
