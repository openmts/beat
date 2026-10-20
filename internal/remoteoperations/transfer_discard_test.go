package remoteoperations

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func countRows(t *testing.T, f fixture, query string, args ...any) int {
	t.Helper()
	var count int
	if err := f.database.DB.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func artifactOf(t *testing.T, f fixture, transferID string) string {
	t.Helper()
	var artifactID string
	if err := f.database.DB.QueryRow("SELECT artifact_id FROM operation_transfers WHERE id=?", transferID).
		Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	return artifactID
}

func uploadDraft(t *testing.T, f fixture, content []byte) TransferSummary {
	t.Helper()
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", content))
	if err != nil {
		t.Fatal(err)
	}
	chunk := ChunkInput{TransferID: transfer.ID, Index: 0, Digest: fileDigest(content), Content: content}
	if err := f.service.UploadChunk(t.Context(), f.actor, chunk); err != nil {
		t.Fatal(err)
	}
	return transfer
}

func TestDiscardTransferDraftRemovesChunksTargetsAndArtifact(t *testing.T) {
	f, directory := fileFixture(t)
	content := []byte("draft-payload")
	transfer := uploadDraft(t, f, content)
	artifactID := artifactOf(t, f, transfer.ID)
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("spool entries = %d, err = %v", len(entries), err)
	}
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); err != nil {
		t.Fatal(err)
	}
	checks := map[string]struct {
		query string
		args  []any
	}{
		"transfer row":   {"SELECT COUNT(*) FROM operation_transfers WHERE id=?", []any{transfer.ID}},
		"target rows":    {"SELECT COUNT(*) FROM operation_transfer_targets WHERE transfer_id=?", []any{transfer.ID}},
		"artifact":       {"SELECT COUNT(*) FROM operation_artifacts WHERE id=?", []any{artifactID}},
		"artifact chunk": {"SELECT COUNT(*) FROM operation_artifact_chunks WHERE artifact_id=?", []any{artifactID}},
	}
	for name, check := range checks {
		if count := countRows(t, f, check.query, check.args...); count != 0 {
			t.Fatalf("%s survived the discard: %d", name, count)
		}
	}
	if remaining, err := os.ReadDir(directory); err != nil || len(remaining) != 0 {
		t.Fatalf("spool kept the draft chunks: %d, err = %v", len(remaining), err)
	}
	if count := countRows(t, f,
		"SELECT COUNT(*) FROM admin_audit_events WHERE action='operation.transfer.discarded' AND resource_id=?",
		transfer.ID); count != 1 {
		t.Fatalf("discard audit rows = %d", count)
	}
	if count := countRows(t, f,
		"SELECT COUNT(*) FROM admin_audit_events WHERE action='operation.transfer.created' AND resource_id=?",
		transfer.ID); count != 1 {
		t.Fatalf("discard dropped the creation evidence: %d", count)
	}
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second discard = %v", err)
	}
}

func TestDiscardTransferDraftRefusesTakenOverAndTerminalDrafts(t *testing.T) {
	f, _ := fileFixture(t)
	finalized := uploadDraft(t, f, []byte("handed to an agent"))
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, finalized.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, finalized.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("discarding a queued transfer = %v", err)
	}
	if count := countRows(t, f, "SELECT COUNT(*) FROM operation_transfers WHERE id=?", finalized.ID); count != 1 {
		t.Fatal("conflicting discard removed the queued transfer")
	}
	terminal := uploadDraft(t, f, []byte("canceled draft"))
	f.sql(t, "UPDATE operation_transfers SET state='canceled' WHERE id=?", terminal.ID)
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, terminal.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("discarding a terminal transfer = %v", err)
	}
	if count := countRows(t, f, "SELECT COUNT(*) FROM operation_transfers WHERE id=?", terminal.ID); count != 1 {
		t.Fatal("terminal transfer was deleted through the draft path")
	}
}

func TestDiscardTransferDraftRefusesScannedAndSharedArtifacts(t *testing.T) {
	f, _ := fileFixture(t)
	scanned := uploadDraft(t, f, []byte("scanning"))
	f.sql(t, "UPDATE operation_artifacts SET scan_id='scan-1' WHERE id=?", artifactOf(t, f, scanned.ID))
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, scanned.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("discarding a scanning artifact = %v", err)
	}
	second := uploadDraft(t, f, []byte("shared"))
	shared := artifactOf(t, f, second.ID)
	f.sql(t, "UPDATE operation_artifacts SET scan_id='' WHERE id=?", artifactOf(t, f, scanned.ID))
	f.sql(t, "UPDATE operation_transfers SET artifact_id=? WHERE id=?", artifactOf(t, f, scanned.ID), second.ID)
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, second.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("discarding a shared artifact = %v", err)
	}
	if count := countRows(t, f, "SELECT COUNT(*) FROM operation_transfers WHERE id=?", second.ID); count != 1 {
		t.Fatal("shared-artifact discard deleted the transfer")
	}
	if count := countRows(t, f, "SELECT COUNT(*) FROM operation_artifacts WHERE id=?", shared); count != 1 {
		t.Fatal("shared-artifact discard left the artifact in an unknown state")
	}
}

func TestDiscardTransferDraftRequiresSpoolAuthorityAndDatabase(t *testing.T) {
	f, _ := fileFixture(t)
	transfer := uploadDraft(t, f, []byte("guarded"))
	if err := f.service.DiscardTransferDraft(t.Context(), Actor{}, transfer.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized discard = %v", err)
	}
	spool := f.service.spool
	f.service.spool = nil
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("discard without a spool = %v", err)
	}
	f.service.spool = spool
	if err := f.database.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("discard against a closed database = %v", err)
	}
}

func TestDiscardTransferDraftReportsUnremovableChunks(t *testing.T) {
	f, directory := fileFixture(t)
	transfer := uploadDraft(t, f, []byte("stuck chunk"))
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("spool entries = %d, err = %v", len(entries), err)
	}
	// 把分块文件换成一个非空目录，spool.remove 会失败：这一轮必须整体回滚，而不是留下半个删除。
	chunk := filepath.Join(directory, entries[0].Name())
	if err := os.Remove(chunk); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(chunk, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chunk, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DiscardTransferDraft(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unremovable chunk = %v", err)
	}
	if count := countRows(t, f, "SELECT COUNT(*) FROM operation_transfers WHERE id=?", transfer.ID); count != 1 {
		t.Fatal("failed discard still deleted the transfer row")
	}
	if count := countRows(t, f,
		"SELECT COUNT(*) FROM admin_audit_events WHERE action='operation.transfer.discarded' AND resource_id=?",
		transfer.ID); count != 0 {
		t.Fatal("failed discard wrote an audit row")
	}
}
