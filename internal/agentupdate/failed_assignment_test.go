//go:build linux

package agentupdate

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCorruptDownloadReportsFailureWithOriginalVersion(t *testing.T) {
	f := newFixture(t)
	observations := 0
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Length", "512")
			w.Header().Set("Content-Range", "bytes 0-511/512")
			w.WriteHeader(http.StatusPartialContent)
			_, err := w.Write(make([]byte, 512))
			if err != nil {
				t.Error(err)
			}
			return
		}
		observations++
		var value map[string]any
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
		}
		if value["outcome"] != "failed" || value["version"] != "v1.0.0" {
			t.Error(value)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 200}); err != nil {
			t.Error(err)
		}
	})
	f.u.config.ServerURL = client.fetcher.TrustedServer()
	f.u.config.Fetcher = client.fetcher
	bootstrapFixture(t, f)
	must(t, f.u.Enqueue(f.assignment))
	supervisor := Supervisor{}
	wantError(t, ErrIntegrity, supervisor.applyPending(t.Context(), f.u, f.assignment))
	if observations != 1 || f.read(t).Phase != "canceled" {
		t.Fatal("failure was not recorded")
	}
	f.checkCurrent(t, f.old)
	pending, err := f.u.Pending()
	must(t, err)
	if pending != nil {
		t.Fatal("failed mailbox blocks owner retry")
	}
}

func TestDiscardDoesNotRemoveANewerAssignment(t *testing.T) {
	f := newFixture(t)
	must(t, f.u.discardPending(f.assignment))
	bootstrapFixture(t, f)
	must(t, f.u.Enqueue(f.assignment))
	stale := f.assignment
	stale.ID = "another-attempt"
	wantError(t, ErrBusy, f.u.discardPending(stale))
	pending, err := f.u.Pending()
	must(t, err)
	if pending == nil {
		t.Fatal("new assignment removed")
	}
	path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, pendingAssignmentFile)
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	wantError(t, ErrJournal, f.u.discardPending(f.assignment))
}
