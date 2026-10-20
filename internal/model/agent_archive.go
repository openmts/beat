package model

import (
	"errors"
	"math"
	"time"
)

// AgentArchiveEnd 只扩展事件时间上界；调用方必须在同一冻结窗口生成 cutoff 和导出。
func AgentArchiveEnd(cutoff time.Time) (time.Time, error) {
	maximum := time.Unix(0, math.MaxInt64).Add(-MaximumAgentClockSkew)
	if cutoff.IsZero() || !cutoff.After(time.Unix(0, 0)) || cutoff.After(maximum) {
		return time.Time{}, errors.New("invalid agent archive cutoff")
	}
	return cutoff.Add(MaximumAgentClockSkew), nil
}
