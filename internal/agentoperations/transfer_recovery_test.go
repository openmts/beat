//go:build linux

package agentoperations

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func preparedFileJournal(t *testing.T, fixture *fileFixture) journal {
	t.Helper()
	fixture.server.mu.Lock()
	fixture.server.started = true
	lease := fixture.server.lease
	fixture.server.mu.Unlock()
	record := journal{Version: 1, State: "running", Lease: lease, Transfer: newTransferProgress(lease)}
	if err := fixture.runner.journals.save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestPublishingCrashReconcilesExactFileWithoutASecondDownload(t *testing.T) {
	for _, phase := range []string{"before-rename", "after-rename", "changed-target"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newFileFixture(t, "deploy", []byte("verified content"))
			record := preparedFileJournal(t, fixture)
			job := transferJob{record: &record, root: fixture.config.FileRoots[0], gateway: fixture.client, save: func() error {
				if record.Transfer.Phase == "published" {
					return ErrStorage
				}
				if err := fixture.runner.journals.save(record); err != nil {
					return err
				}
				if phase == "before-rename" && record.Transfer.Phase == "publishing" {
					return ErrStorage
				}
				return nil
			}}
			if err := job.deploy(t.Context()); !errors.Is(err, ErrStorage) {
				t.Fatalf("crash point missed: %v", err)
			}
			persisted, err := fixture.runner.journals.load("file-attempt")
			if err != nil || persisted.Transfer.Phase != "publishing" {
				t.Fatal("publishing intent not durable")
			}
			before := fixture.server.snapshot().downloads
			if phase == "changed-target" {
				writeFixturePayload(t, fixture, []byte("operator replacement"))
			}
			if err := fixture.runner.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if fixture.server.snapshot().downloads != before {
				t.Fatal("resume downloaded again after verification")
			}
			expected := "succeeded"
			if phase == "changed-target" {
				expected = "interrupted"
			}
			if fixture.server.snapshot().results[0].State != expected {
				t.Fatal("publication outcome incorrectly inferred")
			}
			actual, err := os.ReadFile(filepath.Join(fixture.root, "payload.bin"))
			if err != nil {
				t.Fatal(err)
			}
			want := []byte("verified content")
			if phase == "changed-target" {
				want = []byte("operator replacement")
			}
			if !bytes.Equal(want, actual) {
				t.Fatal("resume overwrote an unrelated destination")
			}
		})
	}
}

func TestResumeRevalidatesEveryPersistedPartialChunk(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		fixture := newFileFixture(t, "deploy", bytes.Repeat([]byte("p"), protocol.FileChunkBytes+37))
		record := preparedFileJournal(t, fixture)
		job := transferJob{record: &record, root: fixture.config.FileRoots[0], gateway: fixture.client, save: func() error {
			if err := fixture.runner.journals.save(record); err != nil {
				return err
			}
			if len(record.Transfer.ChunkDigests) == 1 {
				return ErrTransport
			}
			return nil
		}}
		if err := job.deploy(t.Context()); !errors.Is(err, ErrTransport) {
			t.Fatal("partial download interruption missed")
		}
		temporary := filepath.Join(fixture.root, record.Transfer.Temporary)
		file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("uncommitted tail")); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			file, err := os.OpenFile(temporary, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteAt([]byte("bad"), 0); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err := fixture.runner.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		expected := "succeeded"
		if corrupt {
			expected = "failed"
		}
		if fixture.server.snapshot().results[0].State != expected {
			t.Fatal("partial state was not revalidated")
		}
		if !corrupt && fixture.server.snapshot().downloads != 2 {
			t.Fatal("committed chunk was needlessly downloaded again")
		}
	}
}

func TestRestoreTransferDeadlineAndDurableCommitOutcomes(t *testing.T) {
	for _, state := range []string{"leased", "running"} {
		fixture := newFileFixture(t, "deploy", []byte("data"))
		record := journal{Version: 1, State: state, Lease: fixture.server.lease, Transfer: newTransferProgress(fixture.server.lease)}
		record.Transfer.StartedAt = time.Now().Add(-2 * time.Second).UnixMilli()
		record.Transfer.DeadlineAt = time.Now().Add(-time.Second).UnixMilli()
		if state == "running" {
			fixture.server.configure(func(server *fileServer) { server.started = true })
		}
		if err := fixture.runner.journals.save(record); err != nil {
			t.Fatal(err)
		}
		if err := fixture.runner.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if fixture.server.snapshot().results[0].State != "timed_out" || fixture.server.snapshot().downloads != 0 {
			t.Fatal("expired transfer mutated a destination")
		}
	}
	fixture := newFileFixture(t, "deploy", []byte("data"))
	record := preparedFileJournal(t, fixture)
	record.Transfer.Phase = "published"
	record.Transfer.StartedAt = time.Now().Add(-2 * time.Second).UnixMilli()
	record.Transfer.DeadlineAt = time.Now().Add(-time.Second).UnixMilli()
	if err := fixture.runner.journals.save(record); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.server.snapshot().results[0].State != "succeeded" {
		t.Fatal("durable commit was relabeled as timeout")
	}
}

func TestExplicitOverwriteAndExecutableMode(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("new executable"))
	fixture.server.configure(func(server *fileServer) {
		server.lease.Payload.File.Overwrite = true
		server.lease.Payload.File.Mode = 0o700
	})
	writeFixturePayload(t, fixture, []byte("old"))
	if err := fixture.runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(fixture.root, "payload.bin"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("authorized executable mode missing")
	}
	if fixture.server.snapshot().results[0].State != "succeeded" {
		t.Fatal("explicit overwrite rejected")
	}
}
