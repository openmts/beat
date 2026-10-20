package diagnostics

import (
	"context"

	"github.com/beat/backend/internal/model"
)

// CancelJob 立即生效：排队中的作业直接取消，运行中的作业请求 Agent 终止。
func (service *Service) CancelJob(ctx context.Context, actor Actor, id string) error {
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		job, err := tx.job(id)
		if err != nil {
			return err
		}
		if actor.Role != "owner" && job.ActorUserID != actor.UserID {
			return ErrForbidden
		}
		if job.CompletedAt != nil {
			return ErrConflict
		}
		if job.State == model.DiagnosticStateQueued {
			if err := tx.one(`UPDATE diagnostic_jobs SET state='cancelled', cancel_requested=1, completed_at=?,
				updated_at=? WHERE id=? AND completed_at IS NULL`, tx.now, tx.now, job.ID); err != nil {
				return err
			}
			if err := tx.releaseBudget(job.SourceNodeID, job.ReservedBytes); err != nil {
				return err
			}
		} else if err := tx.one(`UPDATE diagnostic_jobs SET cancel_requested=1, updated_at=? WHERE id=? AND completed_at IS NULL`,
			tx.now, job.ID); err != nil {
			return err
		}
		return tx.audit(actor.UserID, "diagnostic.job.cancelled", job.ID)
	})
}

// RetryJob 为终态作业创建一次新的尝试；unknown 与 cancelled 只有 owner 能重试。
func (service *Service) RetryJob(ctx context.Context, actor Actor, id string) (model.DiagnosticJob, error) {
	var job model.DiagnosticJob
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		current, err := tx.job(id)
		if err != nil {
			return err
		}
		if err := retryable(current, actor); err != nil {
			return err
		}
		policy, err := tx.policy()
		if err != nil {
			return err
		}
		target, err := tx.target(current.TargetID)
		if err != nil {
			return err
		}
		document, digest, err := tx.capability(current.SourceNodeID)
		if err != nil {
			return err
		}
		if !allowsKindIn(document.Kinds, current.Kind) {
			return ErrCapability
		}
		reserved := estimateJobBytes(current)
		if err := tx.reserveBudget(policy, target, current.SourceNodeID, reserved); err != nil {
			return err
		}
		if err := tx.one(`UPDATE diagnostic_jobs SET state='queued', cancel_requested=0, failure_code='',
			completed_at=NULL, started_at=NULL, leased_at=NULL, cursor_sequence=0, actual_bytes=0,
			reserved_bytes=?, capability_digest=?, updated_at=? WHERE id=? AND completed_at IS NOT NULL`,
			reserved, digest, tx.now, current.ID); err != nil {
			return err
		}
		if err := tx.exec(`DELETE FROM diagnostic_hop_identities WHERE job_id=?`, current.ID); err != nil {
			return err
		}
		if err := tx.audit(actor.UserID, "diagnostic.job.retried", current.ID); err != nil {
			return err
		}
		updated, err := tx.job(current.ID)
		if err != nil {
			return err
		}
		job = updated
		return nil
	})
	return job, err
}

func retryable(job model.DiagnosticJob, actor Actor) error {
	switch job.State {
	case model.DiagnosticStateFailed, model.DiagnosticStateTimedOut, model.DiagnosticStateInterrupted, model.DiagnosticStatePartial:
		return nil
	case model.DiagnosticStateUnknown, model.DiagnosticStateCancelled:
		if actor.Role == "owner" {
			return nil
		}
		return ErrForbidden
	default:
		return ErrConflict
	}
}

// Reconcile 过期排队作业、回收未启动的租约，并把失联的运行尝试标记为 unknown。
func (service *Service) Reconcile(ctx context.Context) error {
	return service.write(ctx, func(tx transaction) error {
		if err := tx.reconcileQueue(); err != nil {
			return err
		}
		return tx.reconcileAttempts()
	})
}

func (tx transaction) reconcileQueue() error {
	if err := tx.exec(`UPDATE diagnostic_budget_ledger SET reserved_bytes=MAX(0, reserved_bytes-(
			SELECT COALESCE(SUM(j.reserved_bytes),0) FROM diagnostic_jobs j WHERE j.source_node_id=diagnostic_budget_ledger.node_id
			AND j.state='queued' AND (j.cancel_requested=1 OR j.created_at<=?)
		)) WHERE EXISTS(SELECT 1 FROM diagnostic_jobs j WHERE j.source_node_id=diagnostic_budget_ledger.node_id
			AND j.state='queued' AND (j.cancel_requested=1 OR j.created_at<=?))`,
		tx.now.Add(-QueueLifetime), tx.now.Add(-QueueLifetime)); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE diagnostic_jobs SET state='cancelled', completed_at=?, updated_at=?
		WHERE state='queued' AND cancel_requested=1`, tx.now, tx.now); err != nil {
		return err
	}
	return tx.exec(`UPDATE diagnostic_jobs SET state='timed_out', failure_code='queue_expired',
		completed_at=?, updated_at=? WHERE state='queued' AND cancel_requested=0 AND created_at<=?`,
		tx.now, tx.now, tx.now.Add(-QueueLifetime))
}

func (tx transaction) reconcileAttempts() error {
	if err := tx.exec(`UPDATE diagnostic_jobs SET state='queued', leased_at=NULL, attempt=MAX(0, attempt-1), updated_at=?
		WHERE state='leased' AND id IN (SELECT job_id FROM diagnostic_attempts WHERE state='leased'
			AND started_at IS NULL AND leased_at<=?)`, tx.now, tx.now.Add(-leaseDuration)); err != nil {
		return err
	}
	if err := tx.exec(`DELETE FROM diagnostic_attempts WHERE state='leased' AND started_at IS NULL AND leased_at<=?`,
		tx.now.Add(-leaseDuration)); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE diagnostic_attempts SET state='unknown' WHERE state='running' AND heartbeat_at IS NOT NULL
		AND heartbeat_at<=?`, tx.now.Add(-heartbeatGrace)); err != nil {
		return err
	}
	return tx.exec(`UPDATE diagnostic_jobs SET state='unknown', updated_at=? WHERE state='running' AND id IN
		(SELECT job_id FROM diagnostic_attempts WHERE state='unknown')`, tx.now)
}

// DeleteJob 先删除 MTS 序列，再删除 SQLite 元数据；MTS 失败时作业保持可重试。
func (service *Service) DeleteJob(ctx context.Context, actor Actor, id string) error {
	if service.metrics == nil {
		return ErrUnavailable
	}
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		job, err := tx.job(id)
		if err != nil {
			return err
		}
		if job.CompletedAt == nil {
			return ErrConflict
		}
		if err := service.metrics.DeleteDiagnosticJob(ctx, job.ID); err != nil {
			return ErrUnavailable
		}
		if err := tx.exec(`DELETE FROM diagnostic_hop_identities WHERE job_id=?`, job.ID); err != nil {
			return err
		}
		if err := tx.exec(`DELETE FROM diagnostic_attempts WHERE job_id=?`, job.ID); err != nil {
			return err
		}
		if err := tx.one(`DELETE FROM diagnostic_jobs WHERE id=?`, job.ID); err != nil {
			return err
		}
		return tx.audit(actor.UserID, "diagnostic.job.deleted", job.ID)
	})
}
