package agentrollout

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

type ResumeInput struct {
	ID                     string
	AcknowledgeUncertainty bool
}

func (s *Service) Promote(ctx context.Context, actor Actor, id string) error {
	return s.mutate(ctx, actor, func(tx *transaction) error {
		rollout, err := tx.rollout(id)
		if err != nil {
			return err
		}
		if !promotable(rollout) {
			return ErrConflict
		}
		if _, _, err := s.releaseInTransaction(tx, rollout.ReleaseID); err != nil {
			return err
		}
		if rollout.CurrentWave >= 0 {
			if err := tx.waveHealthy(rollout); err != nil {
				return err
			}
		}
		next := rollout.CurrentWave + 1
		state := "active"
		if next >= rollout.TotalWaves {
			state = "completed"
			next = rollout.CurrentWave
		}
		if err := tx.exec("UPDATE agent_rollouts SET current_wave=?,state=?,reason='',updated_at=? WHERE id=?", next, state, tx.at, id); err != nil {
			return err
		}
		return tx.event(audit{Action: "rollout.promote", Kind: "rollout", ID: id})
	})
}

func (s *Service) Pause(ctx context.Context, actor Actor, id string) error {
	return s.control(ctx, actor, controlInput{id: id, state: "paused", reason: "operator_pause"})
}

func (s *Service) Abort(ctx context.Context, actor Actor, id string) error {
	return s.control(ctx, actor, controlInput{id: id, state: "aborted", reason: "operator_abort"})
}

type controlInput struct{ id, state, reason string }

func (s *Service) control(ctx context.Context, actor Actor, input controlInput) error {
	return s.mutate(ctx, actor, func(tx *transaction) error {
		rollout, err := tx.rollout(input.id)
		if err != nil {
			return err
		}
		if rollout.State == "completed" || rollout.State == "aborted" {
			return ErrConflict
		}
		if err := tx.exec("UPDATE agent_rollouts SET state=?,reason=?,updated_at=? WHERE id=?", input.state, input.reason, tx.at, input.id); err != nil {
			return err
		}
		// 已发放的分配保留nonce以接受最终状态，不把中止误称为撤销已发生的更新。
		if input.state == "aborted" {
			if err := tx.exec("UPDATE agent_rollout_targets SET state='aborted',reason='operator_abort',updated_at=? WHERE rollout_id=? AND state IN('pending','held')", tx.at, input.id); err != nil {
				return err
			}
		}
		return tx.event(audit{Action: "rollout." + input.state, Kind: "rollout", ID: input.id, Reason: input.reason})
	})
}

func (s *Service) Resume(ctx context.Context, actor Actor, input ResumeInput) error {
	return s.mutate(ctx, actor, func(tx *transaction) error {
		rollout, err := tx.rollout(input.ID)
		if err != nil {
			return err
		}
		if rollout.State != "paused" || rollout.CurrentWave < 0 {
			return ErrConflict
		}
		if rollout.Reason != "operator_pause" && !input.AcknowledgeUncertainty {
			return ErrConflict
		}
		if _, _, err := s.releaseInTransaction(tx, rollout.ReleaseID); err != nil {
			return err
		}
		if err := tx.exec("UPDATE agent_rollouts SET state='active',reason='',updated_at=? WHERE id=?", tx.at, input.ID); err != nil {
			return err
		}
		return tx.event(audit{Action: "rollout.resume", Kind: "rollout", ID: input.ID})
	})
}

func (tx *transaction) waveHealthy(rollout Rollout) error {
	rows, err := tx.db.QueryContext(tx.ctx, `SELECT t.state,t.healthy_since,t.last_report_at,n.agent_version,n.last_seen,r.version FROM agent_rollout_targets t JOIN agent_rollouts o ON o.id=t.rollout_id JOIN agent_releases r ON r.id=o.release_id LEFT JOIN nodes n ON n.id=t.node_id WHERE t.rollout_id=? AND t.wave=?`, rollout.ID, rollout.CurrentWave)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var state, expected string
		var version sql.NullString
		var since, report, lastSeen sql.NullTime
		if err := rows.Scan(&state, &since, &report, &version, &lastSeen, &expected); err != nil {
			return ErrUnavailable
		}
		count++
		if !validWaveHealth(waveHealth{state: state, expected: expected, version: version, since: since, report: report, lastSeen: lastSeen}, rollout.Policy, tx.at) {
			return ErrConflict
		}
	}
	if err := rows.Err(); err != nil {
		return ErrUnavailable
	}
	if count == 0 {
		return ErrConflict
	}
	return nil
}

func (tx *transaction) event(event audit) error {
	if err := tx.exec("INSERT INTO agent_rollout_events(id,rollout_id,kind,reason,created_at) VALUES(?,?,?,?,?)", uuid.NewString(), event.ID, event.Action, event.Reason, tx.at); err != nil {
		return err
	}
	return tx.audit(event)
}

func promotable(rollout Rollout) bool {
	return rollout.State != "completed" && rollout.State != "aborted" && (rollout.State != "paused" || rollout.Reason == "awaiting_promotion")
}

type waveHealth struct {
	state, expected         string
	version                 sql.NullString
	since, report, lastSeen sql.NullTime
}

func validWaveHealth(value waveHealth, policy Policy, at time.Time) bool {
	return value.version.Valid && protocol.SameReleaseVersion(value.version.String, value.expected) && value.lastSeen.Valid && at.Sub(value.lastSeen.Time) <= time.Duration(policy.ReportFreshnessSeconds)*time.Second && value.state == "healthy" && value.since.Valid && value.report.Valid && at.Sub(value.since.Time) >= time.Duration(policy.ObservationSeconds)*time.Second && at.Sub(value.report.Time) <= time.Duration(policy.ReportFreshnessSeconds)*time.Second
}
