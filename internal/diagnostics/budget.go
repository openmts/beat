package diagnostics

import (
	"time"

	"github.com/beat/backend/internal/model"
)

const bitsPerByte = 8

const megabitsPerSecond = 1_000_000

// utcDay 是预算账本的日期键；UTC 保证跨节点结算一致。
func utcDay(at time.Time) string { return at.UTC().Format("2006-01-02") }

// estimateJobBytes 预估一次作业的传输字节上界；MTR 不产生可计费流量。
func estimateJobBytes(job model.DiagnosticJob) int64 {
	if job.Kind != model.DiagnosticKindIperf3 {
		return 0
	}
	streams := int64(max(job.ParallelStreams, 1))
	bits := int64(job.BitrateMbps) * megabitsPerSecond * int64(job.DurationSeconds) * streams
	return bits / bitsPerByte
}

// reserveBudget 在排队前事务性预留全局与目标级每日字节预算。
func (tx transaction) reserveBudget(policy model.DiagnosticPolicy, target model.DiagnosticTarget, nodeID string, estimate int64) error {
	if estimate <= 0 {
		return nil
	}
	day := utcDay(tx.now)
	if policy.DailyByteBudget > 0 {
		var used int64
		if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COALESCE(SUM(reserved_bytes + actual_bytes),0)
			FROM diagnostic_budget_ledger WHERE day=?`, day).Scan(&used); err != nil {
			return ErrUnavailable
		}
		if exceedsBudget(used, estimate, policy.DailyByteBudget) {
			return ErrBudget
		}
	}
	if target.DailyByteBudget > 0 {
		var used int64
		if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COALESCE(SUM(reserved_bytes + actual_bytes),0) FROM diagnostic_jobs
			WHERE target_id=? AND kind='iperf3' AND substr(created_at,1,10)=?`, target.ID, day).Scan(&used); err != nil {
			return ErrUnavailable
		}
		if exceedsBudget(used, estimate, target.DailyByteBudget) {
			return ErrBudget
		}
	}
	return tx.one(`INSERT INTO diagnostic_budget_ledger(day, node_id, reserved_bytes, actual_bytes)
		VALUES(?,?,?,0) ON CONFLICT(day, node_id) DO UPDATE SET reserved_bytes=excluded.reserved_bytes`,
		day, nodeID, estimate)
}

// settleBudget 用最终实际字节回填预留；结算不允许产生负预算。作业没有预留行时
// （例如 MTR 不产生可计费流量）也要按实际字节落账，不能让完成流程失败。
func (tx transaction) settleBudget(nodeID string, reserved, actual int64) error {
	if reserved <= 0 && actual <= 0 {
		return nil
	}
	settled, released := max(actual, 0), max(reserved, 0)
	return tx.one(`INSERT INTO diagnostic_budget_ledger(day, node_id, reserved_bytes, actual_bytes)
		VALUES(?,?,0,?) ON CONFLICT(day, node_id) DO UPDATE SET
		reserved_bytes=MAX(0, reserved_bytes-?), actual_bytes=actual_bytes+?`,
		utcDay(tx.now), nodeID, settled, released, settled)
}

// releaseBudget 在排队作业过期或被拒绝时归还预留；缺失的账本行按零预留处理。
func (tx transaction) releaseBudget(nodeID string, reserved int64) error {
	if reserved <= 0 {
		return nil
	}
	return tx.one(`INSERT INTO diagnostic_budget_ledger(day, node_id, reserved_bytes, actual_bytes)
		VALUES(?,?,0,0) ON CONFLICT(day, node_id) DO UPDATE SET reserved_bytes=MAX(0, reserved_bytes-?)`,
		utcDay(tx.now), nodeID, max(reserved, 0))
}

func exceedsBudget(used, estimate int64, limit int64) bool {
	return limit > 0 && used+estimate > limit
}
