package remoteoperations

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func leaseAndStart(t *testing.T, f fixture) AttemptRequest {
	t.Helper()
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	return request
}

// startDeployAttempt 走管理员定稿流程后启动部署尝试。
func startDeployAttempt(t *testing.T, f fixture, transferID string) AttemptRequest {
	t.Helper()
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transferID); err != nil {
		t.Fatalf("finalize transfer: %v", err)
	}
	return leaseAndStart(t, f)
}

// startCollectAttempt 采集方向由 agent 侧定稿，因此只创建转账后直接租约启动。
func startCollectAttempt(t *testing.T, f fixture) (AttemptRequest, string) {
	t.Helper()
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "collect", nil))
	if err != nil {
		t.Fatalf("create collect transfer: %v", err)
	}
	if transfer.ID == "" {
		t.Fatal("missing collect transfer")
	}
	return leaseAndStart(t, f), transfer.ID
}

// TestAgentAttemptResolutionFaults 覆盖 agent 请求解析尝试时的未知尝试、无转账与陈旧指针分支。
func TestAgentAttemptResolutionFaults(t *testing.T) {
	t.Run("unknown attempt", func(t *testing.T) {
		f, _ := fileFixture(t)
		request := AttemptRequest{Node: NodeProof{Token: f.token}, ID: uuid.NewString(), Nonce: "nonce"}
		if _, err := f.service.AgentFileInfo(t.Context(), request); !errors.Is(err, ErrNotFound) {
			t.Fatalf("agent file info unknown attempt = %v", err)
		}
		if err := f.service.CollectSource(t.Context(), request, SourceInfo{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("collect unknown attempt = %v", err)
		}
		if err := f.service.UploadAgentChunk(t.Context(), AgentChunk{Attempt: request}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("upload unknown attempt = %v", err)
		}
		if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("download unknown attempt = %v", err)
		}
	})
	t.Run("task without transfer", func(t *testing.T) {
		f := testFixture(t)
		if id := f.task(t, "attempt-without-transfer"); id == "" {
			t.Fatal("missing shell task")
		}
		request := f.lease(t)
		if err := f.service.Start(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.AgentFileInfo(t.Context(), request); !errors.Is(err, ErrNotFound) {
			t.Fatalf("shell attempt transfer lookup = %v", err)
		}
	})
	t.Run("transfer table unavailable", func(t *testing.T) {
		f, _ := fileFixture(t)
		request, _ := startCollectAttempt(t, f)
		f.sql(t, `ALTER TABLE operation_transfers RENAME TO cov_transfers_backup`)
		if _, err := f.service.AgentFileInfo(t.Context(), request); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unscannable transfer lookup = %v", err)
		}
	})
	t.Run("stale current attempt", func(t *testing.T) {
		f, _ := fileFixture(t)
		request, _ := startCollectAttempt(t, f)
		f.sql(t, `UPDATE operation_task_targets SET current_attempt='stale-attempt'
			WHERE id=(SELECT target_id FROM operation_attempts WHERE id=?)`, request.ID)
		if _, err := f.service.AgentFileInfo(t.Context(), request); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale current attempt = %v", err)
		}
	})
}

// TestCollectSourceStateFaults 覆盖采集源上报在状态与元数据不匹配时的拒绝分支。
func TestCollectSourceStateFaults(t *testing.T) {
	t.Run("source conflict after upload", func(t *testing.T) {
		f, _ := fileFixture(t)
		content := []byte("collect-source-payload")
		request, _ := startCollectAttempt(t, f)
		source := SourceInfo{Size: int64(len(content)), Digest: fileDigest(content)}
		if err := f.service.CollectSource(t.Context(), request, source); err != nil {
			t.Fatal(err)
		}
		other := SourceInfo{Size: int64(len(content)), Digest: fileDigest([]byte("different-payload!!"))}
		if err := f.service.CollectSource(t.Context(), request, other); !errors.Is(err, ErrConflict) {
			t.Fatalf("conflicting source metadata = %v", err)
		}
	})
	t.Run("deploy direction rejected", func(t *testing.T) {
		f, _ := fileFixture(t)
		content := []byte("deploy-direction-payload")
		transferID, _ := seedUploadedChunk(t, f, content)
		request := startDeployAttempt(t, f, transferID)
		if err := f.service.CollectSource(t.Context(), request, SourceInfo{}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("collect on deploy transfer = %v", err)
		}
	})
}

// TestUploadAgentChunkConflictFaults 覆盖 agent 分块上传与既有分块冲突时的失败关闭。
func TestUploadAgentChunkConflictFaults(t *testing.T) {
	f, _ := fileFixture(t)
	content := []byte("collect-chunk-conflict")
	request, transferID := startCollectAttempt(t, f)
	source := SourceInfo{Size: int64(len(content)), Digest: fileDigest(content)}
	if err := f.service.CollectSource(t.Context(), request, source); err != nil {
		t.Fatal(err)
	}
	chunk := AgentChunk{Attempt: request, Index: 0, Digest: fileDigest(content), Content: content}
	if err := f.service.UploadAgentChunk(t.Context(), chunk); err != nil {
		t.Fatal(err)
	}
	conflicting := make([]byte, len(content))
	other := AgentChunk{Attempt: request, Index: 0, Digest: fileDigest(conflicting), Content: conflicting}
	if err := f.service.UploadAgentChunk(t.Context(), other); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting agent chunk = %v", err)
	}
	var state string
	if err := f.database.DB.QueryRow(`SELECT state FROM operation_artifacts WHERE id=(SELECT artifact_id FROM operation_transfers WHERE id=?)`, transferID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "corrupt" {
		t.Fatalf("artifact state after conflict = %q", state)
	}
}

// TestDownloadAgentChunkStateFaults 覆盖 agent 分块下载在扫描中、待检与索引越界时的拒绝分支。
func TestDownloadAgentChunkStateFaults(t *testing.T) {
	t.Run("scanning", func(t *testing.T) {
		f, _ := fileFixture(t)
		content := []byte("download-scanning")
		transferID, _ := seedUploadedChunk(t, f, content)
		request := startDeployAttempt(t, f, transferID)
		f.sql(t, `UPDATE operation_artifacts SET state='scanning' WHERE id=(SELECT artifact_id FROM operation_transfers WHERE id=?)`, transferID)
		if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request}); !errors.Is(err, ErrBusy) {
			t.Fatalf("download while scanning = %v", err)
		}
	})
	t.Run("scan required but not clean", func(t *testing.T) {
		f, _ := fileFixture(t)
		content := []byte("download-unclean")
		transferID, _ := seedUploadedChunk(t, f, content)
		request := startDeployAttempt(t, f, transferID)
		f.sql(t, `UPDATE operation_transfers SET require_scan=1 WHERE id=?`, transferID)
		f.sql(t, `UPDATE operation_artifacts SET state='ready',scan_status='pending' WHERE id=(SELECT artifact_id FROM operation_transfers WHERE id=?)`, transferID)
		if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request}); !errors.Is(err, ErrBusy) {
			t.Fatalf("download with unclean scan = %v", err)
		}
	})
	t.Run("index beyond artifact", func(t *testing.T) {
		f, _ := fileFixture(t)
		content := []byte("download-range")
		transferID, _ := seedUploadedChunk(t, f, content)
		request := startDeployAttempt(t, f, transferID)
		if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request, Index: 1}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("out of range download = %v", err)
		}
	})
}
