//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestTransferFilesystemAuthorityFailures(t *testing.T) {
	if root, err := openTransferRoot("/beat-missing-root-fixture"); err == nil {
		_ = root.Close()
		t.Fatal("missing root accepted")
	}
	directory := privateTestDir(t)
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if root, err := openTransferRoot(directory); err == nil {
		_ = root.Close()
		t.Fatal("public root accepted")
	}
	link := filepath.Join(privateTestDir(t), "root-link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if root, err := openTransferRoot(link); err == nil {
		_ = root.Close()
		t.Fatal("symlink root accepted")
	}
	if safeTransferFilesystem(-1) {
		t.Fatal("bad filesystem handle accepted")
	}
	if root, err := openTransferRoot(isolatedCgroup(t)); err == nil {
		_ = root.Close()
		t.Fatal("virtual cgroup filesystem accepted for file data")
	}
}

func TestSourceAndPublicationRejectInvalidatedHandles(t *testing.T) {
	directory := privateTestDir(t)
	root, err := openTransferRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := root.destination("target")
	if err != nil {
		t.Fatal(err)
	}
	name, err := temporaryName()
	if err != nil {
		t.Fatal(err)
	}
	file, err := destination.temporary(name, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateTransferFile(file, root.device); err == nil {
		t.Fatal("closed transfer handle accepted")
	}
	if _, err := transferFileIdentity(file); err == nil {
		t.Fatal("closed identity handle accepted")
	}
	if _, err := transferFileVersion(file); err == nil {
		t.Fatal("closed version handle accepted")
	}
	if err := setTransferMode(file, 0o600); err == nil {
		t.Fatal("closed mode handle accepted")
	}
	if transferModeMatches(file, 0o600) {
		t.Fatal("closed handle matched mode")
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	if err := destination.removeTemporary(name); err == nil {
		t.Fatal("cleanup failure ignored")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.openSource("source"); err == nil {
		t.Fatal("closed root accepted")
	}
}

func TestTransferLimitsGatewaysAndUncertainCleanup(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("data"))
	lease := fixture.server.lease
	bad := lease
	bad.Payload.File = nil
	if err := fixture.runner.validateFileLease(bad); err == nil {
		t.Fatal("missing file accepted")
	}
	bad = lease
	copySpec := *bad.Payload.File
	copySpec.RootID = "unknown"
	bad.Payload.File = &copySpec
	if err := fixture.runner.validateFileLease(bad); err == nil {
		t.Fatal("unknown root accepted")
	}
	fixture.runner.gateway = &fakeGateway{}
	if err := fixture.runner.validateFileLease(lease); !errors.Is(err, ErrBoundary) {
		t.Fatal("missing file transport accepted")
	}
	if _, err := fixture.runner.newTransferJob(&journal{Lease: lease}); !errors.Is(err, ErrBoundary) {
		t.Fatal("missing file gateway not reported")
	}
	record := journal{Lease: bad, Transfer: &transferProgress{Temporary: ".beat-transfer-" + strings.Repeat("a", 32)}}
	if err := fixture.runner.cleanupTransfer(record); err == nil {
		t.Fatal("unrecognized cleanup root accepted")
	}
	record.Lease = lease
	if err := os.Chmod(fixture.root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.cleanupTransfer(record); err == nil {
		t.Fatal("unsafe cleanup root accepted")
	}
	if err := os.Chmod(fixture.root, 0o700); err != nil {
		t.Fatal(err)
	}
	record.Lease.Payload.File.RelativePath = "absent/target"
	if err := fixture.runner.cleanupTransfer(record); err == nil {
		t.Fatal("missing cleanup parent accepted")
	}
}

func TestPublicationValidationAndCanceledPrefixVerification(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("expected"))
	record := preparedFileJournal(t, fixture)
	job := transferJob{record: &record, root: fixture.config.FileRoots[0], gateway: fixture.client, save: func() error { return nil }}
	file, err := os.CreateTemp(privateTestDir(t), "partial-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write([]byte("wrong")); err != nil {
		t.Fatal(err)
	}
	if err := job.preparePublication(t.Context(), file); !errors.Is(err, ErrFileIntegrity) {
		t.Fatal("unverified payload prepared for publication")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	record.Transfer.ChunkDigests = []string{strings.Repeat("0", 64)}
	if err := job.verifyPrefix(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled verification continued")
	}
	record.Transfer.ChunkDigests = make([]string, chunkCount(record.Lease.Payload.File.Size)+1)
	if err := job.verifyPrefix(t.Context(), file); !errors.Is(err, ErrFileIntegrity) {
		t.Fatal("oversized prefix accepted")
	}
	record.Transfer.ChunkDigests = nil
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := job.preparePublication(t.Context(), file); !errors.Is(err, ErrStorage) {
		t.Fatal("unreadable payload prepared")
	}
	if err := job.verifyPrefix(t.Context(), file); err == nil {
		t.Fatal("closed prefix handle accepted")
	}
	if err := job.uploadSourceChunk(t.Context(), file, FileChunk{Index: 0, Total: 2}); err == nil {
		t.Fatal("unreadable collect source uploaded")
	}
	if err := job.uploadSourceChunk(ctx, file, FileChunk{Index: 0, Total: 2}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled upload continued")
	}
	if _, err := chunkLength(protocol.MaximumFileBytes+1, 0); err == nil {
		t.Fatal("unbounded file accepted")
	}
}
