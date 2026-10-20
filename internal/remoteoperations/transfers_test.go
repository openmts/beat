package remoteoperations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func fileFixture(t *testing.T) (fixture, string) {
	t.Helper()
	f := testFixture(t)
	directory := filepath.Join(t.TempDir(), "spool")
	spool, err := OpenSpool(SpoolConfig{Directory: directory, MaximumBytes: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	f.service.spool = spool
	f.policy.Kinds = []string{"deploy", "collect"}
	f.policy.Roots = []Root{{ID: "inbox", Read: true, Write: true, MaximumBytes: 8 * 1024 * 1024}}
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 1, Name: "Files", Policy: f.policy, NodeIDs: []string{f.nodeID}}); err != nil {
		t.Fatalf("save file policy: %v", err)
	}
	f.digest, err = f.service.Advertise(t.Context(), NodeProof{Token: f.token}, f.policy)
	if err != nil {
		t.Fatalf("advertise file capabilities: %v", err)
	}
	return f, directory
}

func fileDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func transferInput(f fixture, direction string, content []byte) TransferInput {
	return TransferInput{Direction: direction, Filename: "fixture.bin", PolicyID: f.policyID, NodeIDs: []string{f.nodeID}, File: FileSpec{RootID: "inbox", RelativePath: "nested/fixture.bin", Mode: 0o600, Size: int64(len(content)), Digest: fileDigest(content)}}
}

func TestDeploySpoolIsEncryptedResumableAndBoundToAttempt(t *testing.T) {
	f, directory := fileFixture(t)
	content := []byte("private-transfer-content")
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", content))
	if err != nil {
		t.Fatal(err)
	}
	chunk := ChunkInput{TransferID: transfer.ID, Index: 0, Digest: fileDigest(content), Content: content}
	if err := f.service.UploadChunk(t.Context(), f.actor, chunk); err != nil {
		t.Fatal(err)
	}
	if err := f.service.UploadChunk(t.Context(), f.actor, chunk); err != nil {
		t.Fatal("chunk retry", err)
	}
	task, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil || task == "" {
		t.Fatal(err)
	}
	duplicate, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil || duplicate != task {
		t.Fatal("finalize idempotency", err)
	}
	request := f.lease(t)
	if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request, Index: 0}); err != ErrConflict {
		t.Fatal("download before start", err)
	}
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	actual, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: request, Index: 0})
	if err != nil || !bytes.Equal(content, actual) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	if err != nil || bytes.Contains(sealed, content) {
		t.Fatal("spool persisted plaintext")
	}
	info, err := entries[0].Info()
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(err)
	}
}

func TestCollectSpoolRequiresSourceMetadataAndVerifiedFinalization(t *testing.T) {
	f, _ := fileFixture(t)
	content := []byte("collected-private-file")
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "collect", nil))
	if err != nil {
		t.Fatal(err)
	}
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	input := AgentChunk{Attempt: request, Index: 0, Digest: fileDigest(content), Content: content}
	if err := f.service.UploadAgentChunk(t.Context(), input); err != ErrConflict {
		t.Fatal(err)
	}
	source := SourceInfo{Size: int64(len(content)), Digest: fileDigest(content)}
	if err := f.service.CollectSource(t.Context(), request, source); err != nil {
		t.Fatal(err)
	}
	if err := f.service.CollectSource(t.Context(), request, source); err != nil {
		t.Fatal("source retry", err)
	}
	if err := f.service.UploadAgentChunk(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := f.service.FinalizeCollect(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.FinalizeCollect(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = f.service.DownloadTransfer(t.Context(), f.actor, DownloadInput{ID: transfer.ID, Consume: func(info DownloadInfo, reader io.Reader) error {
		if info.Filename != "fixture.bin" || info.Size != int64(len(content)) {
			t.Fatal(info)
		}
		_, err := io.Copy(&output, reader)
		return err
	}})
	if err != nil || !bytes.Equal(content, output.Bytes()) {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded"}}); err != nil {
		t.Fatal(err)
	}
}
