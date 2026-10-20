//go:build linux

package agentoperations

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestTruncatedNetworkDownloadRemainsResumable(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("the complete artifact"))
	fixture.server.configure(func(server *fileServer) { server.truncateDownload = true })
	if err := fixture.runner.RunOnce(t.Context()); !errors.Is(err, ErrTransport) {
		t.Fatalf("mid-body disconnect was not resumable: %v", err)
	}
	if len(fixture.server.snapshot().results) != 0 {
		t.Fatal("network interruption became a terminal integrity failure")
	}
	if err := fixture.runner.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.server.snapshot().results[0].State != "succeeded" {
		t.Fatal("download did not resume")
	}
}

func TestCollectMissingOversizeAndReplacedSourcesAreRejected(t *testing.T) {
	missing := newFileFixture(t, "collect", nil)
	if err := missing.runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if missing.server.snapshot().results[0].State != "failed" {
		t.Fatal("missing source accepted")
	}
	oversized := newFileFixture(t, "collect", nil)
	writeFixturePayload(t, oversized, []byte("larger"))
	oversized.runner.settings.config.FileRoots[0].MaximumBytes = 3
	oversized.runner.settings.policy.Roots[0].MaximumBytes = 3
	if err := oversized.runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if oversized.server.snapshot().results[0].Reason != "local_policy" {
		t.Fatal("oversize source escaped policy")
	}
	replaced := newFileFixture(t, "collect", []byte("original"))
	writeFixturePayload(t, replaced, []byte("original"))
	replaced.server.configure(func(server *fileServer) { server.uploadFailure = 503 })
	if err := replaced.runner.RunOnce(t.Context()); !errors.Is(err, ErrTransport) {
		t.Fatal("upload failure was not retained")
	}
	replacement := filepath.Join(replaced.root, "replacement")
	if err := os.WriteFile(replacement, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(replaced.root, "payload.bin")); err != nil {
		t.Fatal(err)
	}
	if err := replaced.runner.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if replaced.server.snapshot().results[0].Reason != "source_changed" {
		t.Fatal("same-content inode replacement accepted during resume")
	}
}

func TestUntrustedMissingChunkListsAndPolicyResponses(t *testing.T) {
	for _, indices := range [][]int{{-1}, {1000}, {0, 0}, {0, 1, 2}} {
		fixture := newFileFixture(t, "collect", bytes.Repeat([]byte("x"), protocol.FileChunkBytes+1))
		writeFixturePayload(t, fixture, fixture.server.data)
		fixture.server.configure(func(server *fileServer) { server.missingOverride = indices })
		if err := fixture.runner.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if fixture.server.snapshot().results[0].State != "failed" {
			t.Fatal("invalid chunk list accepted")
		}
	}
	for _, status := range []int{400, 403, 404, 409} {
		fixture := newFileFixture(t, "deploy", []byte("artifact"))
		fixture.server.configure(func(server *fileServer) { server.downloadFailure = status })
		if err := fixture.runner.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if fixture.server.snapshot().results[0].State != "failed" {
			t.Fatal("permanent file rejection retried")
		}
	}
}

func TestTransferJournalRejectsInvalidProgressAndClockRollback(t *testing.T) {
	lease := fileLease("deploy", nil)
	valid := newTransferProgress(lease)
	for _, change := range []func(*transferProgress){
		func(p *transferProgress) { p.StartedAt = 0 }, func(p *transferProgress) { p.DeadlineAt = p.StartedAt },
		func(p *transferProgress) { p.Temporary = "../escape" }, func(p *transferProgress) { p.Phase = "unknown" },
		func(p *transferProgress) { p.Source = &protocol.SourceInfo{Size: -1} },
		func(p *transferProgress) { p.ChunkDigests = []string{"bad"} }, func(p *transferProgress) { p.ChunkDigests = make([]string, 4097) },
	} {
		progress := *valid
		change(&progress)
		if err := validateTransferProgress(&progress); err == nil {
			t.Fatal("invalid progress accepted")
		}
	}
	fixture := newFileFixture(t, "deploy", nil)
	record := preparedFileJournal(t, fixture)
	record.Transfer.StartedAt = time.Now().Add(time.Hour).UnixMilli()
	record.Transfer.DeadlineAt = record.Transfer.StartedAt + 1000
	if err := fixture.runner.journals.save(record); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Recover(t.Context()); !errors.Is(err, ErrStorage) {
		t.Fatal("clock rollback extended a transfer")
	}
}

func TestDigestAndPrivateOutputFailuresDoNotClaimSuccess(t *testing.T) {
	directory := privateTestDir(t)
	path := filepath.Join(directory, "source")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := digestFile(ctx, source, 100); !errors.Is(err, context.Canceled) {
		t.Fatal("digest ignored cancellation")
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := digestFile(t.Context(), source, 100); !errors.Is(err, ErrStorage) {
		t.Fatal("closed source was accepted")
	}
	outputCtx, stop := context.WithCancelCause(t.Context())
	defer stop(nil)
	if _, err := openCapture(filepath.Join(directory, "missing"), 10, stop); err == nil {
		t.Fatal("missing output directory accepted")
	}
	if err := os.WriteFile(filepath.Join(directory, "stderr"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openCapture(directory, 10, stop); err == nil {
		t.Fatal("existing output reused")
	}
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = full.Close() }()
	capture := &outputCapture{remaining: 10, cancel: stop}
	if _, err := (outputWriter{capture: capture, file: full}).Write([]byte("private")); err == nil {
		t.Fatal("disk write failure ignored")
	}
	if !errors.Is(context.Cause(outputCtx), ErrStorage) {
		t.Fatal("storage failure did not stop execution")
	}
}
