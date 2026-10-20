package remoteoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTransferMetadataAndMissingChunkReceipts(t *testing.T) {
	f, _ := fileFixture(t)
	if items, err := f.service.ListTransfers(t.Context(), f.actor); err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	transfer := createUploaded(t, f, []byte("source"))
	items, err := f.service.ListTransfers(t.Context(), f.actor)
	if err != nil || len(items) != 1 || items[0].DownloadAvailable {
		t.Fatal(items, err)
	}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != nil {
		t.Fatal(err)
	}
	attempt := f.lease(t)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	info, err := f.service.AgentFileInfo(t.Context(), attempt)
	if err != nil || info.Size != 6 {
		t.Fatal(info, err)
	}
	receipt, err := f.service.AgentFileReceipt(t.Context(), attempt)
	if err != nil || !receipt.Acknowledged || len(receipt.MissingChunks) != 0 {
		t.Fatal(receipt, err)
	}
	attempt.Nonce = "invalid"
	if _, err := f.service.AgentFileReceipt(t.Context(), attempt); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.AgentFileInfo(t.Context(), attempt); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.ListTransfers(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.GetTransfer(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestReadOnlyPolicySummarySupportsAdminAndDisabledStatus(t *testing.T) {
	f := testFixture(t)
	f.sql(t, "UPDATE admin_users SET role='admin'")
	policies, err := f.service.ExecutablePolicies(t.Context(), f.actor)
	if err != nil || len(policies) != 1 {
		t.Fatal(policies, err)
	}
	if _, err := f.service.ListPolicies(t.Context(), f.actor); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.ExecutablePolicies(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.Status(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestSpoolRecoveryResetsOrphanedScanAndDeletesOnlyGeneratedOrphans(t *testing.T) {
	f, directory := fileFixture(t)
	transfer := createUploaded(t, f, []byte("source"))
	f.sql(t, "UPDATE operation_artifacts SET state='scanning',scan_id='orphaned-scan',scan_deadline=?", f.service.now().Add(time.Hour).UnixMilli())
	orphan := uuid.NewString()
	if err := os.WriteFile(filepath.Join(directory, orphan), []byte("encrypted-orphan-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReconcileSpool(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(directory, orphan)); !os.IsNotExist(err) {
		t.Fatal("orphan remains", err)
	}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != nil {
		t.Fatal("scan could not resume", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unowned-name"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Recover(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unrecognized spool entry ignored", err)
	}
}

func TestQueueExpiryIsAtomicWithFailureNotification(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "expire")
	now := f.service.now().Add(QueueLifetime + time.Second)
	f.service.now = func() time.Time { return now }
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state string
	var count int
	if err := f.database.DB.QueryRow("SELECT state FROM operation_task_targets WHERE task_id=?", id).Scan(&state); err != nil || state != "expired" {
		t.Fatal(state, err)
	}
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE event_kind='operation.failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE event_kind='operation.failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("recovery duplicate", count, err)
	}
}

func TestSpoolAndDisabledDependencyBoundaries(t *testing.T) {
	for _, config := range []SpoolConfig{{}, {Directory: t.TempDir(), MaximumBytes: -1}, {Directory: t.TempDir(), MaximumBytes: MaximumSpoolBytes + 1}} {
		if _, err := OpenSpool(config); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	unsafe := filepath.Join(t.TempDir(), "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSpool(SpoolConfig{Directory: unsafe}); err != ErrUnavailable {
		t.Fatal(err)
	}
	f := testFixture(t)
	if _, err := f.service.CreateTransfer(t.Context(), f.actor, TransferInput{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.UploadAgentChunk(t.Context(), AgentChunk{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.CollectSource(t.Context(), AttemptRequest{}, SourceInfo{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.FinalizeCollect(t.Context(), AttemptRequest{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if err := f.service.DownloadTransfer(t.Context(), f.actor, DownloadInput{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	f.service.enabled = false
	if err := f.service.operationMutation(t.Context(), nil); err != ErrForbidden {
		t.Fatal(err)
	}
	f.service.enabled = true
	f.service.queue = nil
	if err := f.service.CancelTransfer(t.Context(), f.actor, "x"); err != ErrUnavailable {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.service.Recover(ctx); err == nil {
		t.Fatal("canceled recovery accepted")
	}
}
