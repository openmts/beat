//go:build linux

package agentoperations

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openReceiptStore(t *testing.T) *journalStore {
	t.Helper()
	store, err := openJournals(filepath.Join(privateTestDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.unlock() })
	return store
}

func TestReceiptsRejectPublicAcknowledgedFile(t *testing.T) {
	store := openReceiptStore(t)
	public := filepath.Join(store.root, ".acknowledged", journalName("attempt-1"))
	if err := os.WriteFile(public, []byte("acknowledged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(public, 0o644); err != nil {
		t.Fatal(err)
	}
	if acknowledged, err := store.acknowledged("attempt-1"); acknowledged || !errors.Is(err, ErrStorage) {
		t.Fatalf("public receipt accepted: %v", err)
	}
	if err := store.markAcknowledged("attempt-1"); !errors.Is(err, ErrStorage) {
		t.Fatalf("mark acknowledged error = %v", err)
	}
}

func TestReceiptsRejectDanglingAcknowledgedSymlink(t *testing.T) {
	store := openReceiptStore(t)
	link := filepath.Join(store.root, ".acknowledged", journalName("attempt-2"))
	if err := os.Symlink(filepath.Join(store.root, "missing-receipt"), link); err != nil {
		t.Fatal(err)
	}
	if err := store.markAcknowledged("attempt-2"); err == nil {
		t.Fatal("dangling receipt symlink accepted")
	}
}

func TestReceiptsRequireAcknowledgedDirectory(t *testing.T) {
	store := openReceiptStore(t)
	if err := os.RemoveAll(filepath.Join(store.root, ".acknowledged")); err != nil {
		t.Fatal(err)
	}
	if err := store.markAcknowledged("attempt-3"); !errors.Is(err, ErrStorage) {
		t.Fatalf("mark acknowledged error = %v", err)
	}
}

func TestDangerousRootSkipsRootEntryForUnrelatedPath(t *testing.T) {
	if dangerousRoot("/tmp/beat-agent-probe", []string{"/"}) {
		t.Fatal("unrelated path rejected")
	}
}

func TestOverlapsRejectsMixedAbsoluteAndRelativePaths(t *testing.T) {
	if !overlaps("relative-path", "/absolute-path") {
		t.Fatal("mixed path shapes accepted")
	}
}

func TestCallbackErrorExposesCode(t *testing.T) {
	if got := callbackError("storage_failure").Error(); got != "storage_failure" {
		t.Fatalf("callback error = %q", got)
	}
}
