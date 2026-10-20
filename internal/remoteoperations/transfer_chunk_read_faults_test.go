package remoteoperations

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func seedUploadedChunk(t *testing.T, f fixture, content []byte) (string, string) {
	t.Helper()
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", content))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.UploadChunk(t.Context(), f.actor,
		ChunkInput{TransferID: transfer.ID, Index: 0, Digest: fileDigest(content), Content: content}); err != nil {
		t.Fatal(err)
	}
	var storage string
	if err := f.database.DB.QueryRow(`SELECT storage_name FROM operation_artifact_chunks
		WHERE artifact_id=(SELECT artifact_id FROM operation_transfers WHERE id=?)`, transfer.ID).Scan(&storage); err != nil {
		t.Fatal(err)
	}
	return transfer.ID, storage
}

func readArtifactChunk(t *testing.T, f fixture, transferID string, index int, dropAccess bool) ([]byte, error) {
	t.Helper()
	var plain []byte
	var readErr error
	if err := f.service.atomicWrite(t.Context(), func(tx transaction) error {
		item, err := tx.artifact(transferID)
		if err != nil {
			readErr = err
			return nil
		}
		if dropAccess {
			tx.access = nil
		}
		plain, readErr = tx.readChunk(f.service.spool, item, index)
		return nil
	}); err != nil {
		t.Fatalf("read chunk transaction: %v", err)
	}
	return plain, readErr
}

// TestReadChunkFaults 覆盖分块读取的缺失、磁盘缺失、密钥不符与长度不符四类损坏判定。
func TestReadChunkFaults(t *testing.T) {
	content := []byte("payload-for-chunk-read")
	t.Run("missing chunk", func(t *testing.T) {
		f, _ := fileFixture(t)
		transferID, _ := seedUploadedChunk(t, f, content)
		if _, err := readArtifactChunk(t, f, transferID, 7, false); !errors.Is(err, errArtifactCorrupt) {
			t.Fatalf("missing chunk error = %v", err)
		}
	})
	t.Run("missing sealed file", func(t *testing.T) {
		f, directory := fileFixture(t)
		transferID, storage := seedUploadedChunk(t, f, content)
		if err := os.Remove(filepath.Join(directory, storage)); err != nil {
			t.Fatal(err)
		}
		if _, err := readArtifactChunk(t, f, transferID, 0, false); !errors.Is(err, errArtifactCorrupt) {
			t.Fatalf("missing sealed file error = %v", err)
		}
	})
	t.Run("length mismatch", func(t *testing.T) {
		f, _ := fileFixture(t)
		transferID, _ := seedUploadedChunk(t, f, content)
		f.sql(t, `UPDATE operation_artifact_chunks SET byte_size=1`)
		if _, err := readArtifactChunk(t, f, transferID, 0, false); !errors.Is(err, errArtifactCorrupt) {
			t.Fatalf("length mismatch error = %v", err)
		}
	})
	t.Run("spool access unavailable", func(t *testing.T) {
		f, _ := fileFixture(t)
		transferID, _ := seedUploadedChunk(t, f, content)
		if _, err := readArtifactChunk(t, f, transferID, 0, true); !errors.Is(err, errArtifactCorrupt) {
			t.Fatalf("missing spool access error = %v", err)
		}
	})
	t.Run("intact chunk", func(t *testing.T) {
		f, _ := fileFixture(t)
		transferID, _ := seedUploadedChunk(t, f, content)
		plain, err := readArtifactChunk(t, f, transferID, 0, false)
		if err != nil || string(plain) != string(content) {
			t.Fatalf("intact chunk = %q, %v", plain, err)
		}
	})
}

// TestReadChunkMutationStopsCorruptTransfer 覆盖损坏分块被转化为停止传输的失败结果。
func TestReadChunkMutationStopsCorruptTransfer(t *testing.T) {
	f, _ := fileFixture(t)
	transferID, _ := seedUploadedChunk(t, f, []byte("payload-for-guard"))
	var outcome chunkReadOutcome
	var request any
	if err := f.service.atomicWrite(t.Context(), func(tx transaction) error {
		item, err := tx.artifact(transferID)
		if err != nil {
			return err
		}
		result, queue, err := tx.readChunkMutation(f.service.spool, guardedChunk{
			actor: f.actor, item: item, transferID: transferID, index: 9})
		outcome, request = result, queue
		return err
	}); err != nil {
		t.Fatalf("corrupt chunk mutation = %v", err)
	}
	if !outcome.corrupt || request == nil {
		t.Fatalf("corrupt outcome = %+v, request = %v", outcome, request)
	}
}

// TestScannerProcessCancellation 覆盖扫描进程的未启动、运行中与已退出三种取消结果。
func TestScannerProcessCancellation(t *testing.T) {
	command := exec.CommandContext(t.Context(), "sleep", "30")
	configureScannerProcess(command)
	if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("cancel before start = %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := command.Cancel(); err != nil {
		t.Fatalf("cancel running scanner = %v", err)
	}
	_ = command.Wait()
	if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("cancel after exit = %v", err)
	}
}

// TestFileDispatchAdmissionRejections 覆盖文件派发的过期、部署未就绪与回收状态非法三类拒绝。
func TestFileDispatchAdmissionRejections(t *testing.T) {
	f, _ := fileFixture(t)
	content := []byte("admission-payload")
	deploy, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", content))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.UploadChunk(t.Context(), f.actor,
		ChunkInput{TransferID: deploy.ID, Index: 0, Digest: fileDigest(content), Content: content}); err != nil {
		t.Fatal(err)
	}
	deployTask, err := f.service.FinalizeTransfer(t.Context(), f.actor, deploy.ID)
	if err != nil || deployTask == "" {
		t.Fatalf("deploy finalize = %q, %v", deployTask, err)
	}
	admission := func(taskID string) error {
		var checkErr error
		if err := f.service.atomicWrite(t.Context(), func(tx transaction) error {
			checkErr = tx.fileDispatchAllowed(taskID)
			return nil
		}); err != nil {
			t.Fatalf("admission transaction: %v", err)
		}
		return checkErr
	}
	f.sql(t, `UPDATE operation_artifacts SET expires_at=? WHERE id=(SELECT artifact_id FROM operation_transfers WHERE task_id=?)`,
		f.service.now().Add(-time.Hour).UnixMilli(), deployTask)
	if err := admission(deployTask); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired artifact admission = %v", err)
	}
	f.sql(t, `UPDATE operation_artifacts SET expires_at=?, state='expired' WHERE id=(SELECT artifact_id FROM operation_transfers WHERE task_id=?)`,
		f.service.now().Add(time.Hour).UnixMilli(), deployTask)
	if err := admission(deployTask); !errors.Is(err, ErrConflict) {
		t.Fatalf("unready deploy admission = %v", err)
	}
	// 回收方向只接受 ready/uploading/awaiting_source；方向列由派发时读回，因此直接改写即可覆盖。
	f.sql(t, `UPDATE operation_transfers SET direction='collect' WHERE task_id=?`, deployTask)
	f.sql(t, `UPDATE operation_artifacts SET state='scanning' WHERE id=(SELECT artifact_id FROM operation_transfers WHERE task_id=?)`, deployTask)
	if err := admission(deployTask); !errors.Is(err, ErrConflict) {
		t.Fatalf("unready collect admission = %v", err)
	}
}

func seedArtifactID(t *testing.T, f fixture) string {
	t.Helper()
	transferID, _ := seedUploadedChunk(t, f, []byte("cleanup-payload"))
	var artifactID string
	if err := f.database.DB.QueryRow(`SELECT artifact_id FROM operation_transfers WHERE id=?`, transferID).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	return artifactID
}

func expireArtifactOnce(t *testing.T, f fixture, spool *Spool, artifactID string) error {
	t.Helper()
	var expireErr error
	if err := f.service.atomicWrite(t.Context(), func(tx transaction) error {
		expireErr = tx.expireArtifact(spool, artifactID)
		return nil
	}); err != nil {
		t.Fatalf("expire artifact transaction: %v", err)
	}
	return expireErr
}

// TestExpireArtifactFaults 覆盖产物作废时清单扫描、游标迭代、暂存删除与转账更新失败。
func TestExpireArtifactFaults(t *testing.T) {
	t.Run("chunk scan failure", func(t *testing.T) {
		f, _ := fileFixture(t)
		artifactID := seedArtifactID(t, f)
		f.sql(t, `ALTER TABLE operation_artifact_chunks RENAME TO cov_chunks_backup`)
		f.sql(t, `CREATE VIEW operation_artifact_chunks AS SELECT artifact_id, NULL AS storage_name FROM cov_chunks_backup`)
		if err := expireArtifactOnce(t, f, f.service.spool, artifactID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unscannable chunk listing = %v", err)
		}
	})
	t.Run("chunk cursor failure", func(t *testing.T) {
		f, _ := fileFixture(t)
		artifactID := seedArtifactID(t, f)
		f.sql(t, `ALTER TABLE operation_artifact_chunks RENAME TO cov_chunks_backup`)
		f.sql(t, `CREATE VIEW operation_artifact_chunks AS SELECT artifact_id,
			CASE WHEN storage_name = 'boom' THEN abs(-9223372036854775808) ELSE storage_name END AS storage_name
			FROM cov_chunks_backup`)
		f.sql(t, `UPDATE cov_chunks_backup SET storage_name='boom' WHERE artifact_id=?`, artifactID)
		if err := expireArtifactOnce(t, f, f.service.spool, artifactID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expired chunk cursor = %v", err)
		}
	})
	t.Run("staging removal failure", func(t *testing.T) {
		f, _ := fileFixture(t)
		artifactID := seedArtifactID(t, f)
		closed, err := OpenSpool(SpoolConfig{Directory: filepath.Join(t.TempDir(), "closed-spool")})
		if err != nil {
			t.Fatal(err)
		}
		if err := closed.Close(); err != nil {
			t.Fatal(err)
		}
		if err := expireArtifactOnce(t, f, closed, artifactID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("closed spool removal = %v", err)
		}
	})
	t.Run("transfer update blocked", func(t *testing.T) {
		f, _ := fileFixture(t)
		artifactID := seedArtifactID(t, f)
		f.sql(t, `CREATE TRIGGER cov_block_transfer_expiry BEFORE UPDATE ON operation_transfers
			BEGIN SELECT RAISE(ABORT,'transfer update blocked'); END`)
		if err := expireArtifactOnce(t, f, f.service.spool, artifactID); err == nil {
			t.Fatal("blocked transfer update was accepted")
		}
	})
}

// TestReconcileSpoolFileFaults 覆盖对账时占位目录不可读与分块清单查询失败。
func TestReconcileSpoolFileFaults(t *testing.T) {
	t.Run("directory unavailable", func(t *testing.T) {
		f, _ := fileFixture(t)
		closed, err := OpenSpool(SpoolConfig{Directory: filepath.Join(t.TempDir(), "closed-spool")})
		if err != nil {
			t.Fatal(err)
		}
		if err := closed.Close(); err != nil {
			t.Fatal(err)
		}
		var reconcileErr error
		if err := f.service.atomicWrite(t.Context(), func(tx transaction) error {
			reconcileErr = tx.reconcileSpoolFiles(closed)
			return nil
		}); err != nil {
			t.Fatalf("reconcile transaction: %v", err)
		}
		if !errors.Is(reconcileErr, ErrUnavailable) {
			t.Fatalf("closed spool reconcile = %v", reconcileErr)
		}
	})
	t.Run("chunk lookup failure", func(t *testing.T) {
		f, directory := fileFixture(t)
		f.sql(t, `ALTER TABLE operation_artifact_chunks RENAME TO cov_chunks_backup`)
		orphan := uuid.NewString()
		if err := os.WriteFile(filepath.Join(directory, orphan), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		var reconcileErr error
		if err := f.service.atomicWrite(t.Context(), func(tx transaction) error {
			reconcileErr = tx.reconcileSpoolFiles(f.service.spool)
			return nil
		}); err != nil {
			t.Fatalf("reconcile transaction: %v", err)
		}
		if !errors.Is(reconcileErr, ErrUnavailable) {
			t.Fatalf("unscannable chunk lookup = %v", reconcileErr)
		}
	})
	t.Run("orphan spool file removed", func(t *testing.T) {
		f, directory := fileFixture(t)
		orphan := uuid.NewString()
		path := filepath.Join(directory, orphan)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := f.service.ReconcileSpool(t.Context()); err != nil {
			t.Fatalf("spool reconcile = %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("orphan spool file survived reconcile: %v", err)
		}
	})
}
