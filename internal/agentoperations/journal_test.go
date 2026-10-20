//go:build linux

package agentoperations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestJournalDurabilityOwnershipAndExclusiveRunner(t *testing.T) {
	root := privateTestDir(t)
	store, err := openJournals(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.unlock() }()
	if _, err := openJournals(store.root); err == nil {
		t.Fatal("two runners acquired same local journal")
	}
	record := journal{Version: 1, State: "leased", Lease: testLease("printf retained")}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.load(record.Lease.AttemptID)
	if err != nil || string(loaded.Lease.Payload.Command) != "printf retained" {
		t.Fatal("durable lease missing")
	}
	if err := store.markAcknowledged(record.Lease.AttemptID); err != nil {
		t.Fatal(err)
	}
	if err := store.markAcknowledged(record.Lease.AttemptID); err != nil {
		t.Fatal(err)
	}
	acknowledged, err := store.acknowledged(record.Lease.AttemptID)
	if err != nil || !acknowledged {
		t.Fatal("receipt missing")
	}
	if err := store.remove(record.Lease.AttemptID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(record.Lease.AttemptID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("acknowledged payload persisted")
	}
}

func TestCorruptJournalNeverBecomesExecutable(t *testing.T) {
	store, err := openJournals(filepath.Join(privateTestDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.unlock() }()
	record := journal{Version: 1, State: "leased", Lease: testLease("true")}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.directory(record.Lease.AttemptID), "journal.json")
	for _, content := range []string{"{}", "invalid", `{"version":1,"extra":true}`, strings.Repeat("x", maximumJournalBytes+1)} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.list(); err == nil {
			t.Fatal("corrupt state accepted")
		}
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(record.Lease.AttemptID); err == nil {
		t.Fatal("public journal accepted")
	}
}

func TestJournalStateAndStorageFailurePaths(t *testing.T) {
	for _, record := range []journal{
		{}, {Version: 1, State: "unknown", Lease: testLease("true")},
		{Version: 1, State: "completed", Lease: testLease("true")},
		{Version: 1, State: "authorized", Lease: testLease("true"), Result: &protocol.Result{}},
	} {
		if err := validateJournal(record); err == nil {
			t.Fatal("invalid durable state accepted")
		}
	}
	store, err := openJournals(filepath.Join(privateTestDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.unlock() }()
	if err := store.save(journal{}); err == nil {
		t.Fatal("invalid journal persisted")
	}
	if err := store.remove("absent"); err == nil {
		t.Fatal("missing state removed without error")
	}
	if _, err := store.loadDirectory("../escape"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := store.loadDirectory(strings.Repeat("z", 64)); err == nil {
		t.Fatal("nonhex state path accepted")
	}
	if err := atomicPrivateWrite(filepath.Join(store.root, "missing", "file"), []byte("x")); err == nil {
		t.Fatal("missing parent accepted")
	}
	if err := os.Mkdir(filepath.Join(store.root, "target"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicPrivateWrite(filepath.Join(store.root, "target"), []byte("x")); err == nil {
		t.Fatal("directory replaced by journal")
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".journal-") {
			t.Fatal("temporary journal leaked")
		}
	}
}
