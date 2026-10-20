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

func TestDeployAndCollectAcrossAuthenticatedHTTP(t *testing.T) {
	for _, size := range []int{0, 23, protocol.FileChunkBytes + 79} {
		data := bytes.Repeat([]byte("x"), size)
		for _, kind := range []string{"deploy", "collect"} {
			t.Run(kind, func(t *testing.T) {
				fixture := newFileFixture(t, kind, data)
				if kind == "collect" {
					writeFixturePayload(t, fixture, data)
				}
				if err := fixture.runner.RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
				if len(fixture.server.snapshot().results) != 1 || fixture.server.snapshot().results[0].State != "succeeded" {
					t.Fatal("transfer did not complete")
				}
				if kind == "deploy" {
					actual, err := os.ReadFile(filepath.Join(fixture.root, "payload.bin"))
					if err != nil || !bytes.Equal(data, actual) {
						t.Fatal("deployed bytes differ")
					}
				} else if fixture.server.snapshot().source.Size != int64(size) || fixture.server.snapshot().finalizes != 1 {
					t.Fatal("collection missing final verification")
				}
				journals, err := fixture.runner.journals.list()
				if err != nil || len(journals) != 0 {
					t.Fatal("acknowledged transfer retained journal")
				}
			})
		}
	}
}

func TestFileStartRejectionDoesNotDownloadOrCreateACompletion(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("private artifact"))
	fixture.server.configure(func(server *fileServer) { server.startStatus = 409 })
	if err := fixture.runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.server.snapshot().downloads != 0 || len(fixture.server.snapshot().results) != 0 {
		t.Fatal("unstarted transfer performed work")
	}
	records, err := fixture.runner.journals.list()
	if err != nil || len(records) != 0 {
		t.Fatal("rejected unstarted lease was retained")
	}
}

func TestExistingDestinationAndCorruptDownloadRemainUnchanged(t *testing.T) {
	for _, failure := range []string{"existing", "range", "digest"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newFileFixture(t, "deploy", []byte("replacement"))
			if failure == "existing" {
				writeFixturePayload(t, fixture, []byte("original"))
			}
			fixture.server.configure(func(server *fileServer) { server.badRange = failure == "range"; server.badDigest = failure == "digest" })
			if err := fixture.runner.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(fixture.server.snapshot().results) != 1 || fixture.server.snapshot().results[0].State != "failed" {
				t.Fatal("invalid transfer accepted")
			}
			if failure == "existing" {
				actual, err := os.ReadFile(filepath.Join(fixture.root, "payload.bin"))
				if err != nil || string(actual) != "original" {
					t.Fatal("existing file replaced")
				}
			}
			entries, err := os.ReadDir(fixture.root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if validTemporaryName(entry.Name()) {
					t.Fatal("failed transfer left plaintext temporary data")
				}
			}
		})
	}
}

func TestDownloadAndFinalizeNetworkFailuresResumeWithoutResettingDeadline(t *testing.T) {
	for _, kind := range []string{"deploy", "collect"} {
		t.Run(kind, func(t *testing.T) {
			data := []byte("stable source")
			fixture := newFileFixture(t, kind, data)
			if kind == "deploy" {
				fixture.server.configure(func(server *fileServer) { server.downloadFailure = 503 })
			} else {
				writeFixturePayload(t, fixture, data)
				fixture.server.configure(func(server *fileServer) { server.finalizeFailure = 503 })
			}
			if err := fixture.runner.RunOnce(t.Context()); !errors.Is(err, ErrTransport) {
				t.Fatal("temporary failure was not retained")
			}
			before, err := fixture.runner.journals.load("file-attempt")
			if err != nil {
				t.Fatal(err)
			}
			if kind == "collect" {
				writeFixturePayload(t, fixture, []byte("changed after final verification"))
			}
			if err := fixture.runner.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(fixture.server.snapshot().results) != 1 || fixture.server.snapshot().results[0].State != "succeeded" {
				t.Fatal("resume did not finish original transfer")
			}
			if fixture.server.snapshot().results[0].StartedAt != before.Transfer.StartedAt {
				t.Fatal("resumed transfer reset its start")
			}
		})
	}
}

func TestSourceMutationAndRemoteCancellationStopTransfers(t *testing.T) {
	fixture := newFileFixture(t, "collect", []byte("source"))
	writeFixturePayload(t, fixture, []byte("source"))
	fixture.server.configure(func(server *fileServer) {
		server.onUpload = func(int) {
			if err := os.WriteFile(filepath.Join(fixture.root, "payload.bin"), []byte("mutated"), 0o600); err != nil {
				t.Error(err)
			}
		}
	})
	if err := fixture.runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.server.snapshot().results[0].Reason != "source_changed" || fixture.server.snapshot().finalizes != 0 {
		t.Fatal("changed source was finalized")
	}
	canceled := newFileFixture(t, "deploy", []byte("artifact"))
	canceled.server.configure(func(server *fileServer) { server.cancel = true; server.delayDownload = 100 * time.Millisecond })
	if err := canceled.runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if canceled.server.snapshot().results[0].State != "canceled" {
		t.Fatal("cancellation ignored")
	}
	if _, err := os.Stat(filepath.Join(canceled.root, "payload.bin")); !os.IsNotExist(err) {
		t.Fatal("canceled deploy published")
	}
}
