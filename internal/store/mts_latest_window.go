package store

import (
	"context"
	"math"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// 上游引擎不会把 Limit(1) 下推到扫描层：[0, now] 的「取最新一条」会完整扫描该
// 节点全部历史样本，单指标约 150ms；一个节点二十多个指标就要数秒，直连请求会
// 在 5s 预算上抖动出 500。这里按由近到远的窗口探测，命中最新样本即停止；窗口
// 全部落空时仍回退到调用方给出的完整范围，长期离线节点取到的依旧是最新值。
const (
	latestWindowProbe     = 15 * time.Minute
	latestWindowRecent    = 2 * time.Hour
	latestWindowDaily     = 12 * time.Hour
	latestWindowWeekly    = 3 * 24 * time.Hour
	latestWindowFortnight = 14 * 24 * time.Hour
)

var latestScanWindows = [...]time.Duration{
	latestWindowProbe, latestWindowRecent, latestWindowDaily, latestWindowWeekly, latestWindowFortnight,
}

// latestScanRange 是一次候选扫描范围。
type latestScanRange struct{ from, to time.Time }

// latestScanPlan 生成由近到远的候选范围：窗口相对 probeEnd 回推，末尾附带调用方
// 给出的完整范围兜底（可能是 [0, MaxInt64] 的全历史，用于覆盖时钟偏快的样本）。
func latestScanPlan(start, end, probeEnd time.Time) []latestScanRange {
	plan := make([]latestScanRange, 0, len(latestScanWindows)+1)
	for _, window := range latestScanWindows {
		from := probeEnd.Add(-window)
		if !from.After(start) || from.After(end) {
			break
		}
		plan = append(plan, latestScanRange{from: from, to: probeEnd})
	}
	return append(plan, latestScanRange{from: start, to: end})
}

// scanLatestRows 按候选范围顺序扫描并返回首个非空结果；不存在时返回空切片。
func scanLatestRows(
	ctx context.Context,
	plan []latestScanRange,
	scan func(context.Context, time.Time, time.Time) ([]mts.Row, error),
) ([]mts.Row, error) {
	for _, span := range plan {
		rows, err := scan(ctx, span.from, span.to)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}
	}
	return nil, nil
}

// latestProbeEnd 把 math.MaxInt64 这类「至今」的上界收敛成可探测的当前时间，
// 并保留时钟偏移余量，避免时钟略快的 agent 样本被排除；其他上界原样返回。
func latestProbeEnd(end int64) time.Time {
	if end == math.MaxInt64 {
		return time.Now().UTC().Add(model.MaximumAgentClockSkew)
	}
	return time.Unix(0, end).UTC()
}

// latestBound 把纳秒上界还原成时间，MaxInt64 仍然表示「至今」。
func latestBound(end int64) time.Time {
	return time.Unix(0, end).UTC()
}
