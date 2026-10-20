package agentrollout

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

type observedTarget struct {
	id, rolloutID, state, reason string
	since, lastReport, expires   sql.NullTime
	nonce                        []byte
}

func (s *Service) Observe(ctx context.Context, identity model.AgentIdentity, input protocol.Observation) error {
	if !validObservation(input, s.now()) {
		return ErrInvalid
	}
	return s.agentMutation(ctx, identity, func(tx *transaction, node nodeState) error {
		target, err := tx.observedTarget(identity.NodeID, input)
		if err != nil {
			return err
		}
		rollout, err := tx.rollout(target.rolloutID)
		if err != nil {
			return err
		}
		return tx.recordObservation(observationWrite{target: target, rollout: rollout, node: node, input: input})
	})
}

type observationWrite struct {
	target  observedTarget
	rollout Rollout
	node    nodeState
	input   protocol.Observation
}

func validObservation(input protocol.Observation, now time.Time) bool {
	if !validObservationIdentity(input, now) {
		return false
	}
	if input.Outcome != "healthy" && input.Outcome != "failed" && input.Outcome != "rolled_back" {
		return false
	}
	for _, count := range []int{input.Restarts, input.ProtocolErrors, input.MetricErrors, input.TaskFailures} {
		if count < 0 || count > maximumObservationCounter {
			return false
		}
	}
	return true
}

func validObservationIdentity(input protocol.Observation, now time.Time) bool {
	return input.AssignmentID != "" && len(input.AssignmentID) <= maximumIdentifierBytes && len(input.Nonce) == 2*assignmentNonceBytes && len(input.Version) <= maximumIdentifierBytes && !input.ReportAt.IsZero() && !input.ReportAt.After(now.Add(time.Second))
}

func (tx *transaction) observedTarget(nodeID string, input protocol.Observation) (observedTarget, error) {
	var target observedTarget
	err := tx.db.QueryRowContext(tx.ctx, `SELECT id,rollout_id,state,reason,healthy_since,last_report_at,lease_expires_at,nonce_hash FROM agent_rollout_targets WHERE node_id=? AND current_attempt_id=?`, nodeID, input.AssignmentID).Scan(&target.id, &target.rolloutID, &target.state, &target.reason, &target.since, &target.lastReport, &target.expires, &target.nonce)
	if err != nil {
		return target, ErrNotFound
	}
	hash := sha256.Sum256([]byte(input.Nonce))
	if subtle.ConstantTimeCompare(target.nonce, hash[:]) != 1 || !target.expires.Valid || (target.state != "healthy" && target.state != "failed" && !tx.at.Before(target.expires.Time)) {
		return target, ErrForbidden
	}
	if target.lastReport.Valid && input.ReportAt.Before(target.lastReport.Time) {
		return target, ErrConflict
	}
	return target, nil
}

func (tx *transaction) recordObservation(write observationWrite) error {
	state, reason, since, err := tx.observationState(write)
	if err != nil {
		return err
	}
	redacted := write.input
	redacted.Nonce = ""
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return ErrInvalid
	}
	if err := tx.exec(`UPDATE agent_rollout_targets SET state=?,reason=?,healthy_since=?,last_report_at=?,lease_expires_at=?,observation_json=?,updated_at=? WHERE id=?`, state, reason, since, write.input.ReportAt, write.target.expires, string(encoded), tx.at, write.target.id); err != nil {
		return err
	}
	if err := tx.updateAttemptState(state, string(encoded), write); err != nil {
		return err
	}
	if state == "failed" {
		if err := tx.exec("UPDATE agent_rollouts SET state='paused',reason='health_gate_failed',updated_at=? WHERE id=? AND state<>'aborted' AND state<>'completed'", tx.at, write.rollout.ID); err != nil {
			return err
		}
	}
	if state != write.target.state {
		return tx.targetEvent(write.target.id, "assignment."+state, reason)
	}
	if write.target.state == "healthy" && thresholdFailure(write.input, write.rollout.Policy, 0) != "" {
		recorded, err := tx.transientEventRecorded(write.target.id)
		if err != nil || recorded {
			return err
		}
		return tx.targetEvent(write.target.id, "assignment.healthy", "transient_error_tolerated")
	}
	return nil
}

// transientEventRecorded 判断该目标是否已经记过一次瞬时错误预算事件。预算每次尝试只会被用掉
// 一次，而 Agent 的计数是累计值——healthy 之后的每个上报周期都会继续命中同一个越界，不判重就会
// 每个周期多写一条同名事件（宿主演练实测 5 条/25 秒）。
func (tx *transaction) transientEventRecorded(targetID string) (bool, error) {
	var count int
	err := tx.db.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM agent_rollout_events WHERE target_id=? AND kind='assignment.healthy' AND reason='transient_error_tolerated'`, targetID).Scan(&count)
	return count > 0, err
}

// maximumRestartBudget 与前端策略表单的上限一致，避免显式巨大值加一后溢出。
const maximumRestartBudget = 1_000_000

// restartBudget 策略里的 max_restarts 是「额外」重启预算：Agent 的观测计数已经把
// 升级自身那一次重启算进去了（见 agentupdate 的 journal.Restarts），这里补回这一次。
func restartBudget(policy Policy) int {
	if policy.MaxRestarts >= maximumRestartBudget {
		return maximumRestartBudget
	}
	return policy.MaxRestarts + 1
}

// observationFailure 是不带瞬时预算的严格判定：金丝雀在观察窗口内（尚未 healthy 时）使用它。
func observationFailure(input protocol.Observation, policy Policy) string {
	return thresholdFailure(input, policy, 0)
}

// thresholdFailure 把 Agent 自报的计数与策略阈值比较，budget 为额外容忍的瞬时越界次数。
func thresholdFailure(input protocol.Observation, policy Policy, budget int) string {
	if input.Outcome == "rolled_back" {
		return "agent_rolled_back"
	}
	if input.Outcome == "failed" {
		return "agent_failed"
	}
	if input.Restarts > restartBudget(policy)+budget {
		return "restart_threshold"
	}
	if input.ProtocolErrors > policy.MaxProtocolErrors+budget {
		return "protocol_error_threshold"
	}
	if input.MetricErrors > policy.MaxMetricErrors+budget {
		return "metric_error_threshold"
	}
	if input.TaskFailures > policy.MaxTaskFailures+budget {
		return "task_failure_threshold"
	}
	return ""
}

// toleratedTransientErrors 是目标通过健康门禁之后的瞬时错误预算：升级窗口内偶发的一次限流或抖动
// （例如一次 429 让 Agent 记下一次 metric_error）不应该推翻已经 healthy 的金丝雀，因此再多容忍
// 一次越界；Agent 显式上报的 failed/rolled_back 不受预算保护。
const toleratedTransientErrors = 1

func transientErrorBudget(state string) int {
	if state == "healthy" {
		return toleratedTransientErrors
	}
	return 0
}

// updateAttemptState 写回尝试状态。状态没变时不动 ended_at：终态 first-wins 的语义要求「第一次判失败
// 的时刻」不被后续上报周期刷新，否则排障时会看到一个不断后移的结束时间。
func (tx *transaction) updateAttemptState(state, encoded string, write observationWrite) error {
	if state == write.target.state {
		return tx.exec("UPDATE agent_rollout_attempts SET state=?,observation_json=? WHERE id=?", state, encoded, write.input.AssignmentID)
	}
	return tx.exec("UPDATE agent_rollout_attempts SET state=?,observation_json=?,ended_at=CASE WHEN ? IN('healthy','failed') THEN ? ELSE NULL END WHERE id=?", state, encoded, state, tx.at, write.input.AssignmentID)
}

func (tx *transaction) observationState(write observationWrite) (string, string, sql.NullTime, error) {
	if write.target.state == "failed" {
		// 终态 first-wins：Agent 的计数是累计值，判失败后的每个上报周期都会命中（可能不同的）阈值；
		// 保留第一次判定时写下的原因，排障与审计才不会看到原因被后续周期改写。
		return "failed", write.target.reason, sql.NullTime{}, nil
	}
	reason := thresholdFailure(write.input, write.rollout.Policy, transientErrorBudget(write.target.state))
	if reason != "" {
		return "failed", reason, sql.NullTime{}, nil
	}
	ready, err := tx.reportReady(write)
	if err != nil {
		return "", "", sql.NullTime{}, err
	}
	if !ready {
		return "observing", "waiting_for_valid_report", sql.NullTime{}, nil
	}
	since := write.target.since
	if write.target.lastReport.Valid && tx.at.Sub(write.target.lastReport.Time) > time.Duration(write.rollout.Policy.ReportFreshnessSeconds)*time.Second {
		since = sql.NullTime{}
	}
	if !since.Valid {
		since = sql.NullTime{Time: tx.at, Valid: true}
	}
	state := "observing"
	if tx.at.Sub(since.Time) >= time.Duration(write.rollout.Policy.ObservationSeconds)*time.Second {
		state = "healthy"
	}
	return state, "", since, nil
}

func (tx *transaction) reportReady(write observationWrite) (bool, error) {
	var payload []byte
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT payload FROM agent_releases WHERE id=?", write.rollout.ReleaseID).Scan(&payload); err != nil {
		return false, ErrUnavailable
	}
	manifest, err := decodeManifest(payload)
	if err != nil || releaseDigest(payload) != write.rollout.ReleaseID {
		return false, ErrManifest
	}
	return protocol.SameReleaseVersion(write.node.version, manifest.Version) && protocol.SameReleaseVersion(write.input.Version, manifest.Version) && write.node.lastSeen.Valid && !write.node.lastSeen.Time.Before(write.input.ReportAt) && tx.at.Sub(write.input.ReportAt) <= time.Duration(write.rollout.Policy.ReportFreshnessSeconds)*time.Second, nil
}
