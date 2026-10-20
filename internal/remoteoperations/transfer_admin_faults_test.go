package remoteoperations

import (
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
)

func leaseRequest(f fixture) LeaseRequest {
	return LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}
}

// TestLeaseReconcileFaults 覆盖租约对账在尝试表、目标表不可读与状态清扫被阻断时的失败关闭。
func TestLeaseReconcileFaults(t *testing.T) {
	t.Run("attempt table unavailable", func(t *testing.T) {
		f := testFixture(t)
		if id := f.task(t, "lease-attempts"); id == "" {
			t.Fatal("missing task")
		}
		f.sql(t, `ALTER TABLE operation_attempts RENAME TO cov_attempts_backup`)
		if _, err := f.service.Lease(t.Context(), leaseRequest(f)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("lease with unreadable attempts = %v", err)
		}
	})
	t.Run("target table unavailable", func(t *testing.T) {
		f := testFixture(t)
		if id := f.task(t, "lease-targets"); id == "" {
			t.Fatal("missing task")
		}
		f.sql(t, `ALTER TABLE operation_task_targets RENAME TO cov_targets_backup`)
		if _, err := f.service.Lease(t.Context(), leaseRequest(f)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("lease with unreadable targets = %v", err)
		}
	})
	t.Run("unknown sweep blocked", func(t *testing.T) {
		f := testFixture(t)
		if id := f.task(t, "lease-stale-heartbeat"); id == "" {
			t.Fatal("missing task")
		}
		request := f.lease(t)
		if err := f.service.Start(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		f.sql(t, `UPDATE operation_attempts SET heartbeat_at=0 WHERE id=?`, request.ID)
		f.sql(t, `CREATE TRIGGER cov_block_unknown BEFORE UPDATE ON operation_attempts
			WHEN NEW.state='unknown' BEGIN SELECT RAISE(ABORT,'blocked'); END`)
		if _, err := f.service.Lease(t.Context(), leaseRequest(f)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("lease with blocked sweep = %v", err)
		}
	})
}

// TestDiscardTransferDraftFaults 覆盖弃置草稿在查表、删行、摘引用与分块清理各步的失败关闭。
func TestDiscardTransferDraftFaults(t *testing.T) {
	deploy := func(t *testing.T, name string) (fixture, TransferSummary) {
		t.Helper()
		f, _ := fileFixture(t)
		return f, uploadDraft(t, f, []byte(name))
	}
	t.Run("transfer lookup failure", func(t *testing.T) {
		f, transfer := deploy(t, "discard-lookup")
		f.sql(t, `ALTER TABLE operation_transfers RENAME TO cov_transfers_backup`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unreadable draft lookup = %v", err)
		}
	})
	t.Run("target delete blocked", func(t *testing.T) {
		f, transfer := deploy(t, "discard-targets")
		f.sql(t, `CREATE TRIGGER cov_block_delete BEFORE DELETE ON operation_transfer_targets
			BEGIN SELECT RAISE(ABORT,'blocked'); END`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("blocked target delete = %v", err)
		}
	})
	t.Run("transfer delete blocked", func(t *testing.T) {
		f, transfer := deploy(t, "discard-transfer")
		f.sql(t, `CREATE TRIGGER cov_block_delete BEFORE DELETE ON operation_transfers
			BEGIN SELECT RAISE(ABORT,'blocked'); END`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("blocked transfer delete = %v", err)
		}
	})
	t.Run("artifact update blocked", func(t *testing.T) {
		f, transfer := deploy(t, "discard-update")
		f.sql(t, `CREATE TRIGGER cov_block_update BEFORE UPDATE ON operation_transfers
			BEGIN SELECT RAISE(ABORT,'blocked'); END`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("blocked artifact detach = %v", err)
		}
	})
	t.Run("chunk listing unavailable", func(t *testing.T) {
		f, transfer := deploy(t, "discard-listing")
		f.sql(t, `ALTER TABLE operation_artifact_chunks RENAME TO cov_chunks_backup`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unreadable chunk listing = %v", err)
		}
	})
	t.Run("chunk delete blocked", func(t *testing.T) {
		f, transfer := deploy(t, "discard-chunks")
		f.sql(t, `ALTER TABLE operation_artifact_chunks RENAME TO cov_chunks_backup`)
		f.sql(t, `CREATE VIEW operation_artifact_chunks AS SELECT artifact_id,storage_name FROM cov_chunks_backup`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("blocked chunk delete = %v", err)
		}
	})
	t.Run("chunk name unreadable", func(t *testing.T) {
		f, transfer := deploy(t, "discard-names")
		f.sql(t, `ALTER TABLE operation_artifact_chunks RENAME TO cov_chunks_backup`)
		f.sql(t, `CREATE VIEW operation_artifact_chunks AS SELECT artifact_id,NULL AS storage_name FROM cov_chunks_backup`)
		if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unreadable chunk name = %v", err)
		}
	})
}

// TestCompleteResultFaults 覆盖结果上报的未知尝试、采集未就绪、转账缺失与冲突审计失败。
func TestCompleteResultFaults(t *testing.T) {
	t.Run("unknown attempt", func(t *testing.T) {
		f, _ := fileFixture(t)
		request := AttemptRequest{Node: NodeProof{Token: f.token}, ID: uuid.NewString(), Nonce: "nonce"}
		err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown attempt completion = %v", err)
		}
	})
	t.Run("collect before ready", func(t *testing.T) {
		f, _ := fileFixture(t)
		request, _ := startCollectAttempt(t, f)
		err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("premature collect completion = %v", err)
		}
	})
	t.Run("transfer row missing", func(t *testing.T) {
		f, _ := fileFixture(t)
		content := []byte("complete-missing-transfer")
		transferID, _ := seedUploadedChunk(t, f, content)
		request := startDeployAttempt(t, f, transferID)
		f.sql(t, `UPDATE operation_transfers SET task_id=NULL WHERE id=?`, transferID)
		err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}})
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("completion without transfer = %v", err)
		}
	})
	t.Run("conflict audit blocked", func(t *testing.T) {
		f, directory := fileFixture(t)
		content := []byte("complete-conflict-audit")
		transferID, _ := seedUploadedChunk(t, f, content)
		request := startDeployAttempt(t, f, transferID)
		first := Result{State: "succeeded", Stdout: []byte("first")}
		if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request, Index: 0}); err != nil {
			t.Fatal(err)
		}
		if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: first}); err != nil {
			t.Fatal(err)
		}
		f.sql(t, `CREATE TRIGGER cov_block_audit BEFORE INSERT ON admin_audit_events
			BEGIN SELECT RAISE(ABORT,'blocked'); END`)
		second := Result{State: "succeeded", Stdout: []byte("second")}
		if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: second}); err == nil {
			t.Fatal("conflicting result with blocked audit was accepted")
		}
		if entries, err := os.ReadDir(directory); err != nil || len(entries) == 0 {
			t.Fatalf("spool entries = %v, err = %v", entries, err)
		}
	})
}
