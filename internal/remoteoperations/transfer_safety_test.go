package remoteoperations

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type waitingScanner struct{ started chan struct{} }

func (scanner waitingScanner) Scan(ctx context.Context, reader io.Reader) error {
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return err
	}
	close(scanner.started)
	<-ctx.Done()
	return ctx.Err()
}

func createUploaded(t *testing.T, f fixture, content []byte) TransferSummary {
	t.Helper()
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", content))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > 0 {
		if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: transfer.ID, Index: 0, Digest: fileDigest(content), Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	return transfer
}

func TestScannerDoesNotBlockCancellationOrTransactions(t *testing.T) {
	f, _ := fileFixture(t)
	scanner := waitingScanner{started: make(chan struct{})}
	f.service.spool.scanner = scanner
	transfer := createUploaded(t, f, []byte("scan content"))
	finished := make(chan error, 1)
	go func() { _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); finished <- err }()
	select {
	case <-scanner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("scanner did not start")
	}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != ErrBusy {
		t.Fatal("concurrent scan was not retryable", err)
	}
	canceled := make(chan error, 1)
	go func() { canceled <- f.service.CancelTransfer(t.Context(), f.actor, transfer.ID) }()
	select {
	case err := <-canceled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scanner held transaction during cancellation")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled scan finalized")
		}
	case <-time.After(time.Second):
		t.Fatal("scanner context was not canceled")
	}
	item, err := f.service.GetTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil || item.State != "canceled" {
		t.Fatal(item, err)
	}
}

func TestConflictingChunkFailsEntireSessionAndQueuesOneEvent(t *testing.T) {
	f, _ := fileFixture(t)
	transfer := createUploaded(t, f, []byte("first"))
	other := []byte("other")
	input := ChunkInput{TransferID: transfer.ID, Index: 0, Digest: fileDigest(other), Content: other}
	if err := f.service.UploadChunk(t.Context(), f.actor, input); err != ErrConflict {
		t.Fatal(err)
	}
	if err := f.service.UploadChunk(t.Context(), f.actor, input); err != ErrConflict {
		t.Fatal(err)
	}
	item, err := f.service.GetTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil || item.State != "corrupt" {
		t.Fatal(item, err)
	}
	var count int
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE event_kind='operation.failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_artifact_chunks").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestMissingSpoolAfterRestoreCancelsWorkAndReleasesKeyReferences(t *testing.T) {
	f, directory := fileFixture(t)
	transfer := createUploaded(t, f, []byte("lost source"))
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, entries[0].Name())); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	item, err := f.service.GetTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil || item.State != "corrupt" {
		t.Fatal(item, err)
	}
	lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest})
	if err != nil || lease != nil {
		t.Fatal("missing artifact dispatched", err)
	}
	var count int
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_artifact_chunks").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestSpoolCorruptionNeverReturnsPlaintext(t *testing.T) {
	f, directory := fileFixture(t)
	transfer := createUploaded(t, f, []byte("private"))
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != nil {
		t.Fatal(err)
	}
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(directory, entries[0].Name())
	sealed, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if err := os.WriteFile(name, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request, Index: 0})
	if err != ErrConflict || len(data) != 0 {
		t.Fatal("corrupt frame exposed", err)
	}
}

func TestChunkBoundsQuotaScanRequirementAndEmptyFile(t *testing.T) {
	f, _ := fileFixture(t)
	f.service.spool.maximumBytes = 128
	if _, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", []byte("x"))); err != ErrForbidden {
		t.Fatal("encrypted overhead not reserved", err)
	}
	f.service.spool.maximumBytes = 1024
	f.policy.RequireScan = true
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 2, Name: "Scan", Policy: f.policy, NodeIDs: []string{f.nodeID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", nil)); err != ErrForbidden {
		t.Fatal("missing required scanner", err)
	}
	f.service.spool.scanner = scanAll{}
	transfer := createUploaded(t, f, nil)
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: transfer.ID, Content: []byte("x"), Digest: fileDigest([]byte("x"))}); err != ErrInvalid {
		t.Fatal(err)
	}
}

type scanAll struct{}

func (scanAll) Scan(_ context.Context, reader io.Reader) error {
	_, err := io.Copy(io.Discard, reader)
	return err
}

func TestScannerRejectsUnreadContentAndFixedCommandFailures(t *testing.T) {
	if _, err := NewCommandScanner(CommandScannerConfig{Executable: "relative"}); err != ErrInvalid {
		t.Fatal(err)
	}
	scanner, err := NewCommandScanner(CommandScannerConfig{Executable: "/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := scanner.Scan(t.Context(), bytes.NewBufferString("fixture")); err != nil {
		t.Fatal(err)
	}
	scanner, err = NewCommandScanner(CommandScannerConfig{Executable: "/nonexistent/beat-scanner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := scanner.Scan(t.Context(), bytes.NewReader(nil)); !errors.Is(err, errScanRejected) {
		t.Fatal(err)
	}
}
