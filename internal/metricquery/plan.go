package metricquery

import "time"

// MinimumBucketInterval 是公共历史最小桶宽。更小的桶宽产生的点数没有意义，而且会让
// 时间戳精度依赖调用方的输入。
const MinimumBucketInterval = time.Second

// PlanBuckets 从请求区间推导确定性的桶宽与桶数：桶宽按秒向上取整，桶数不超过点预算。
// 区间非法（非正或超过 31 天）时返回 ok=false。
func PlanBuckets(from, to time.Time, maxPoints int) (time.Duration, int, bool) {
	if !to.After(from) || to.Sub(from) > MaximumRange {
		return 0, 0, false
	}
	if maxPoints < MinimumPoints || maxPoints > MaximumPoints {
		return 0, 0, false
	}
	interval := ceilSeconds((to.Sub(from) + time.Duration(maxPoints) - 1) / time.Duration(maxPoints))
	if interval < MinimumBucketInterval {
		interval = MinimumBucketInterval
	}
	buckets := int((to.Sub(from) + interval - 1) / interval)
	if buckets < 1 || buckets > maxPoints {
		return 0, 0, false
	}
	return interval, buckets, true
}

func ceilSeconds(interval time.Duration) time.Duration {
	if interval%time.Second == 0 {
		return interval
	}
	return interval - interval%time.Second + time.Second
}
