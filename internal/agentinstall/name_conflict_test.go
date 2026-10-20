package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/agentjoin"
)

func TestNameConflictCanRetryWithStableCredentials(t *testing.T) {
	f := newFixture(t)
	var original agentjoin.Request
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		var request agentjoin.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if f.requests.Load() == 1 {
			original = request
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(agentjoin.Envelope[any]{Code: http.StatusConflict, Message: agentjoin.NodeNameConflictMessage})
			return
		}
		if request.RequestID != original.RequestID || request.AgentToken != original.AgentToken || request.NodeName != "unique-node" {
			t.Error("rename retry must preserve enrollment credentials")
		}
		_ = json.NewEncoder(w).Encode(agentjoin.Envelope[agentjoin.Result]{Code: http.StatusOK, Message: "success", Data: f.result})
	}
	if _, err := f.installer.Join(context.Background(), f.options); !errors.Is(err, errNodeNameConflict) {
		t.Fatalf("want name conflict, got %v", err)
	}
	f.options.Name, f.result.Config.NodeName = "unique-node", "unique-node"
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	f.options.Name = "another-node"
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("joined nodes must not be renamed by join")
	}
	if f.requests.Load() != 2 {
		t.Fatalf("want two attempts, got %d", f.requests.Load())
	}
}

func TestOtherConflictDoesNotAllowRename(t *testing.T) {
	f := newFixture(t)
	f.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(agentjoin.Envelope[any]{Code: http.StatusConflict, Message: "token is already used"})
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected token conflict")
	}
	f.options.Name = "different"
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("token conflict must not permit renamed enrollment")
	}
	if f.requests.Load() != 1 {
		t.Fatal("rename retried after unrelated conflict")
	}
}

func TestSavedNameConflictRejectsInvalidNameAndWriteFailure(t *testing.T) {
	f := newFixture(t)
	options, err := f.installer.normalizeOptions(f.options)
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, f.installer.paths.stateDir)
	state, err := f.installer.createEnrollment(options)
	if err != nil {
		t.Fatal(err)
	}
	state.RenameAllowed = true
	if err := f.installer.updatePendingName(state, "\x00"); err == nil {
		t.Fatal("expected invalid name rejection")
	}
	state.RenameAllowed = true
	f.installer.write = func(string, []byte, os.FileMode) error { return errors.New("disk failure") }
	if err := f.installer.updatePendingName(state, "unique-node"); err == nil {
		t.Fatal("expected state write failure")
	}
}

func TestServerPreservesPreviouslyCommittedNodeName(t *testing.T) {
	f := newFixture(t)
	f.options.Name = "retry-name"
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	var state enrollment
	if _, err := loadPrivateJSON(filepath.Join(f.installer.paths.stateDir, "enrollment.json"), &state); err != nil {
		t.Fatal(err)
	}
	if state.Result.Config.NodeName != "test-node" {
		t.Fatal("join must use the server's persisted node name")
	}
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
}

func TestRunningExecutableCopyAndDirectoryProbe(t *testing.T) {
	if data, err := readSourceExecutable("/proc/self/exe"); err != nil || len(data) == 0 {
		t.Fatalf("cannot read running executable: %v", err)
	}
	if err := probeDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing-directory probe error")
	}
}
