package diagnostics

import (
	"context"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// ProgressFrame 把每节点证明与共享线路消息绑定在一起。
type ProgressFrame struct {
	Proof NodeProof
	diagnosticprotocol.ProgressRequest
}

// Completion 把每节点证明与终态报告绑定在一起。
type Completion struct {
	Proof NodeProof
	diagnosticprotocol.CompletionRequest
}

// Progress 先落 MTS 再更新序号，保证确认的序号一定已持久化数值。
func (service *Service) Progress(ctx context.Context, frame ProgressFrame) (int64, error) {
	if frame.Sequence < 1 {
		return 0, ErrInvalid
	}
	var acknowledged int64
	err := service.write(ctx, func(tx transaction) error {
		attempt, job, err := tx.authenticatedAttempt(AttemptRequest{Proof: frame.Proof, JobID: frame.JobID,
			Attempt: frame.Attempt, Nonce: frame.Nonce})
		if err != nil {
			return err
		}
		if attempt.State != "running" && attempt.State != "unknown" {
			return ErrConflict
		}
		if frame.Sequence <= job.CursorSequence {
			acknowledged = job.CursorSequence
			return nil
		}
		if len(frame.Samples) > model.MaximumDiagnosticSampleBatch || len(frame.Hops) > maximumFrameHops {
			return ErrInvalid
		}
		if len(frame.Samples) > 0 {
			if err := service.metrics.WriteDiagnosticSamples(ctx, store.DiagnosticSampleBatch{NodeID: job.SourceNodeID,
				JobID: job.ID, At: tx.now, Samples: bindSamples(job, frame.Samples)}); err != nil {
				return ErrUnavailable
			}
		}
		if len(frame.Hops) > 0 {
			if err := tx.storeHopIdentities(job.ID, frame.Hops); err != nil {
				return err
			}
		}
		if err := tx.one(`UPDATE diagnostic_jobs SET cursor_sequence=?, updated_at=? WHERE id=?
			AND cursor_sequence<?`, frame.Sequence, tx.now, job.ID, frame.Sequence); err != nil {
			return err
		}
		acknowledged = frame.Sequence
		return nil
	})
	return acknowledged, err
}

// Complete 落终态并结算实际字节；到达 running 的尝试绝不自动重放。
func (service *Service) Complete(ctx context.Context, completion Completion) error {
	if completion.ActualBytes < 0 {
		return ErrInvalid
	}
	return service.write(ctx, func(tx transaction) error {
		attempt, job, err := tx.authenticatedAttempt(AttemptRequest{Proof: completion.Proof, JobID: completion.JobID,
			Attempt: completion.Attempt, Nonce: completion.Nonce})
		if err != nil {
			return err
		}
		state, err := finalState(job, attempt, completion.State)
		if err != nil {
			return err
		}
		if job.CompletedAt != nil {
			if job.State == state {
				return nil
			}
			return ErrConflict
		}
		if err := tx.one(`UPDATE diagnostic_attempts SET state='completed', completed_at=?, failure_code=?
			WHERE job_id=? AND attempt=?`, tx.now, completion.FailureCode, job.ID, attempt.Attempt); err != nil {
			return err
		}
		if err := tx.one(`UPDATE diagnostic_jobs SET state=?, actual_bytes=?, failure_code=?, completed_at=?,
			updated_at=? WHERE id=? AND completed_at IS NULL`, state, completion.ActualBytes, completion.FailureCode,
			tx.now, tx.now, job.ID); err != nil {
			return err
		}
		if err := tx.settleBudget(job.SourceNodeID, job.ReservedBytes, completion.ActualBytes); err != nil {
			return err
		}
		return tx.audit(job.ActorUserID, "diagnostic.attempt.completed", job.ID)
	})
}

// finalState 把取消请求与 Agent 报告合并成一个确定的终态。
func finalState(job model.DiagnosticJob, attempt storedAttempt, reported string) (string, error) {
	if job.CancelRequested {
		if reported == model.DiagnosticStateUnknown {
			return model.DiagnosticStateUnknown, nil
		}
		return model.DiagnosticStateCancelled, nil
	}
	if attempt.StartedAt == nil {
		return "", ErrConflict
	}
	switch reported {
	case model.DiagnosticStateSucceeded, model.DiagnosticStatePartial, model.DiagnosticStateFailed,
		model.DiagnosticStateTimedOut, model.DiagnosticStateCancelled, model.DiagnosticStateInterrupted,
		model.DiagnosticStateUnknown:
		return reported, nil
	default:
		return "", ErrInvalid
	}
}

// bindSamples 用已认证作业覆盖样本身份，Agent 不能自行声明作业或节点。
func bindSamples(job model.DiagnosticJob, samples []diagnosticprotocol.Sample) []model.DiagnosticSample {
	bound := make([]model.DiagnosticSample, 0, len(samples))
	for _, sample := range samples {
		bound = append(bound, model.DiagnosticSample{JobID: job.ID, NodeID: job.SourceNodeID,
			Metric: sample.Metric, Hop: sample.Hop, Stat: sample.Stat, Role: sample.Role,
			Direction: sample.Direction, Protocol: sample.Protocol, Value: sample.Value})
	}
	return bound
}
