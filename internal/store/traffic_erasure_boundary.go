package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 擦除相关的流量范围：只有包含流量测量的范围才是新的结算边界。
const latestTrafficErasureCutoffQuery = `SELECT COALESCE(
	(SELECT MAX(cutoff_unix_nano) FROM metric_erasure_jobs
		WHERE state = 'succeeded' AND scope = 'node_history' AND target_id = ?),
	(SELECT MAX(cutoff_unix_nano) FROM metric_erasure_jobs
		WHERE state = 'succeeded' AND scope = 'delete_node' AND target_id = ?),
	(SELECT MAX(cutoff_unix_nano) FROM metric_erasure_jobs
		WHERE state = 'succeeded' AND scope IN ('fleet_telemetry','all_metric_history')))`

// LatestErasureCutoff 返回该节点最近一次成功擦除流量历史的秒级边界。
//
// 擦除过的原始增量不可能再被读回，因此发生在边界之前（含边界）的校准目标不再
// 适用于后续周期结算；它们继续作为不可变审计历史存在。返回 found=false 表示没有
// 覆盖到流量的成功擦除。
func (store *TrafficCalibrationStore) LatestErasureCutoff(
	ctx context.Context,
	nodeID string,
) (time.Time, bool, error) {
	if store == nil || nodeID == "" {
		return time.Time{}, false, nil
	}
	var cutoff sql.NullInt64
	err := store.db.QueryRowContext(ctx, latestTrafficErasureCutoffQuery, nodeID, nodeID).Scan(&cutoff)
	if err != nil {
		if isMissingErasureTable(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("read traffic erasure boundary: %w", err)
	}
	if !cutoff.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(0, cutoff.Int64).UTC(), true, nil
}

func isMissingErasureTable(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no such table")
}
