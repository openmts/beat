//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type afterStartClient struct {
	*Client
	after func()
}

func (client afterStartClient) Start(ctx context.Context, lease protocol.Lease) error {
	if err := client.Client.Start(ctx, lease); err != nil {
		return err
	}
	client.after()
	return nil
}

func TestFileMutationWaitsForDurableRunningState(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("data"))
	fixture.runner.gateway = afterStartClient{Client: fixture.client, after: func() { blockJournalPath(t, fixture.runner, "file-attempt") }}
	if err := fixture.runner.RunOnce(t.Context()); !errors.Is(err, ErrStorage) {
		t.Fatal("running-state write failure ignored")
	}
	if fixture.server.snapshot().downloads != 0 {
		t.Fatal("file fetched before running state was durable")
	}
}

func TestFileResumeHandlesRevokedStartAndTransientAuthorization(t *testing.T) {
	for _, status := range []int{409, 503} {
		fixture := newFileFixture(t, "deploy", []byte("data"))
		_ = preparedFileJournal(t, fixture)
		fixture.server.configure(func(server *fileServer) { server.startStatus = status })
		err := fixture.runner.Recover(t.Context())
		if status == 503 {
			if !errors.Is(err, ErrTransport) {
				t.Fatal("temporary authorization error discarded journal")
			}
			continue
		}
		if err != nil || fixture.server.snapshot().results[0].State != "canceled" {
			t.Fatal("revoked running transfer was not canceled")
		}
	}
}

func TestFileAdmissionAndCleanupFailBeforeUnsafeIO(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("data"))
	bad := journal{Version: 1, State: "leased", Lease: fixture.server.lease}
	bad.Lease.Nonce = "invalid"
	if err := fixture.runner.runTransfer(t.Context(), bad); !errors.Is(err, ErrPolicy) {
		t.Fatal("invalid transfer authorized")
	}
	record := journal{Version: 1, State: "leased", Lease: fixture.server.lease}
	if err := os.WriteFile(fixture.runner.journals.directory("file-attempt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.runTransfer(t.Context(), record); !errors.Is(err, ErrStorage) {
		t.Fatal("progress storage failure ignored")
	}
	if fixture.server.snapshot().starts != 0 {
		t.Fatal("attempt started without progress journal")
	}
	if err := os.Remove(fixture.runner.journals.directory("file-attempt")); err != nil {
		t.Fatal(err)
	}
	record = preparedFileJournal(t, fixture)
	name, err := temporaryName()
	if err != nil {
		t.Fatal(err)
	}
	record.Transfer.Temporary = name
	record.Transfer.Phase = "downloading"
	if err := os.Chmod(fixture.root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.finishTransfer(t.Context(), record, ErrFileIntegrity); err == nil {
		t.Fatal("failed cleanup reported completion")
	}
	if len(fixture.server.snapshot().results) != 0 {
		t.Fatal("unclean state acknowledged")
	}
	if err := os.Chmod(fixture.root, 0o700); err != nil {
		t.Fatal(err)
	}
	unknown := *record.Lease.Payload.File
	unknown.RootID = "unknown"
	record.Lease.Payload.File = &unknown
	if _, err := fixture.runner.newTransferJob(&record); !errors.Is(err, ErrPolicy) {
		t.Fatal("unknown job root accepted")
	}
}

func TestPublicationRecoveryRefusesAlteredIdentityModeAndMissingFiles(t *testing.T) {
	for _, fault := range []string{"identity", "mode", "missing", "symlink", "conflict", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newFileFixture(t, "deploy", []byte("verified"))
			record := preparedFileJournal(t, fixture)
			job := transferJob{record: &record, root: fixture.config.FileRoots[0], gateway: fixture.client, save: func() error { return nil }}
			root, err := openTransferRoot(fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			destination, err := root.destination("payload.bin")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = destination.Close() }()
			file, err := job.prepareTemporary(destination)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write([]byte("verified")); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := job.preparePublication(t.Context(), file); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			temporary := filepath.Join(fixture.root, record.Transfer.Temporary)
			switch fault {
			case "identity":
				record.Transfer.TemporaryIdentity = "unrelated"
			case "mode":
				if err := os.Chmod(temporary, 0o700); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(temporary); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(temporary); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/etc/passwd", temporary); err != nil {
					t.Fatal(err)
				}
			case "conflict":
				writeFixturePayload(t, fixture, []byte("keep"))
			}
			ctx := t.Context()
			if fault == "canceled" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			if err := job.reconcilePublication(ctx, destination); err == nil {
				t.Fatal("unsafe publication resumed")
			}
			if fault == "conflict" {
				content, err := os.ReadFile(filepath.Join(fixture.root, "payload.bin"))
				if err != nil || string(content) != "keep" {
					t.Fatal("conflicting destination changed")
				}
			}
		})
	}
}

func TestChunkWriteAndSyncFailuresNeverAdvanceProgress(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("data"))
	record := preparedFileJournal(t, fixture)
	job := transferJob{record: &record, root: fixture.config.FileRoots[0], gateway: fixture.client, save: func() error { return nil }}
	file, err := os.CreateTemp(privateTestDir(t), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := job.downloadChunk(t.Context(), file, 0); !errors.Is(err, ErrStorage) {
		t.Fatal("failed chunk write was acknowledged")
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Close(); _ = write.Close() }()
	if err := job.downloadChunk(t.Context(), write, 0); !errors.Is(err, ErrStorage) {
		t.Fatal("unsupported durability barrier accepted")
	}
	if len(record.Transfer.ChunkDigests) != 0 {
		t.Fatal("unsynced chunk entered durable progress")
	}
	if _, err := job.prepareTemporary(&transferDestination{}); err == nil {
		t.Fatal("invalid destination accepted")
	}
}
