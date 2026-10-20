//go:build linux

package agentoperations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestPrivateStateRefusesUnsafeLocksAndReceiptDirectories(t *testing.T) {
	for _, kind := range []string{"symlink-lock", "public-lock", "receipt-file", "public-state"} {
		t.Run(kind, func(t *testing.T) {
			directory := privateTestDir(t)
			switch kind {
			case "symlink-lock":
				if err := os.Symlink("/dev/null", filepath.Join(directory, ".lock")); err != nil {
					t.Fatal(err)
				}
			case "public-lock":
				if err := os.WriteFile(filepath.Join(directory, ".lock"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(directory, ".lock"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "receipt-file":
				if err := os.WriteFile(filepath.Join(directory, ".acknowledged"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "public-state":
				if err := os.Chmod(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if store, err := openJournals(directory); err == nil {
				_ = store.unlock()
				t.Fatal("unsafe journal authority accepted")
			}
		})
	}
	if _, err := openJournals(filepath.Join(privateTestDir(t), "absent", "state")); err == nil {
		t.Fatal("missing parent accepted")
	}
	if _, err := lockState(filepath.Join(privateTestDir(t), "missing", "lock")); err == nil {
		t.Fatal("missing lock parent ignored")
	}
	if err := syncDirectory("/beat-agent-no-such-fixture"); err == nil {
		t.Fatal("missing directory sync ignored")
	}
}

func TestJournalDecodeRejectsTrailingDataWrongIdentityAndUnsafeHeaders(t *testing.T) {
	store, err := openJournals(filepath.Join(privateTestDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.unlock() }()
	record := journal{Version: 1, State: "leased", Lease: testLease("true")}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.directory(record.Lease.AttemptID), "journal.json")
	for _, raw := range [][]byte{[]byte("invalid"), append(append([]byte(nil), content...), []byte(" {}")...), []byte(`{"version":99}`)} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.load(record.Lease.AttemptID); err == nil {
			t.Fatal("malformed journal accepted")
		}
	}
	record.Lease.AttemptID = "other"
	content, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load("attempt-1"); err == nil {
		t.Fatal("cross-attempt journal accepted")
	}
	for _, change := range []func(*journal){
		func(j *journal) { j.Lease.Nonce = strings.Repeat("z", 64) },
		func(j *journal) { j.Result = &protocol.Result{State: "succeeded"} },
		func(j *journal) { j.Lease.Payload.Limits.Kind = "deploy"; j.Lease.Payload.File = nil },
		func(j *journal) { j.Transfer = &transferProgress{Phase: "bad"} },
	} {
		candidate := journal{Version: 1, State: "leased", Lease: testLease("true")}
		change(&candidate)
		if err := store.save(candidate); err == nil {
			t.Fatal("unsafe journal header persisted")
		}
	}
}

func TestStorageObstructionsPreventPublishingNewAttempts(t *testing.T) {
	store, err := openJournals(filepath.Join(privateTestDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.unlock() }()
	record := journal{Version: 1, State: "leased", Lease: testLease("true")}
	directory := store.directory(record.Lease.AttemptID)
	if err := os.WriteFile(directory, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.save(record); err == nil {
		t.Fatal("obstructed attempt path replaced")
	}
	retained, err := os.ReadFile(directory)
	if err != nil || string(retained) != "retained" {
		t.Fatal("unrelated object deleted")
	}
	if _, err := store.list(); err == nil {
		t.Fatal("unknown state object accepted")
	}
	if _, err := readBoundedPrivate(filepath.Join(store.root, "missing"), 10); err == nil {
		t.Fatal("missing private input accepted")
	}
	if _, err := store.loadDirectory(strings.Repeat("a", 64)); err == nil {
		t.Fatal("missing hash directory accepted")
	}
}

func TestJournalSizeBoundDoesNotLeaveAnUncommittedAttemptDirectory(t *testing.T) {
	store, err := openJournals(filepath.Join(privateTestDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.unlock() }()
	record := journal{Version: 1, State: "leased", Lease: testLease("true")}
	record.Lease.Payload.NodeIDs = []string{strings.Repeat("x", maximumJournalBytes)}
	if err := store.save(record); err == nil {
		t.Fatal("oversized state accepted")
	}
	if _, err := os.Lstat(store.directory(record.Lease.AttemptID)); !os.IsNotExist(err) {
		t.Fatal("rejected input stranded an uncommitted directory")
	}
}
