package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMTSOwnerLeaseCloseAndVerifyRejectsEmptyLease(t *testing.T) {
	var absent *mtsOwnerLease
	if err := absent.Close(); err != nil {
		t.Fatalf("closing an absent lease must be safe: %v", err)
	}
	empty := &mtsOwnerLease{}
	if err := empty.Close(); err != nil {
		t.Fatalf("closing a lease without a file must be safe: %v", err)
	}
	if err := empty.Verify(); err == nil {
		t.Fatal("a lease without a file must not verify")
	}
}

func TestMTSOwnerLockPathRejectsUnresolvableDirectories(t *testing.T) {
	if _, err := MTSOwnerLockPath(""); err == nil {
		t.Fatal("empty MTS directory must be rejected")
	}
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MTSOwnerLockPath(filepath.Join(blocker, "mts")); err == nil {
		t.Fatal("directory below a regular file must be rejected")
	}
	if _, err := MTSOwnerLockPath(filepath.Join(t.TempDir(), "missing", "mts")); err == nil {
		t.Fatal("directory with a missing parent must be rejected")
	}
}

func TestMTSOwnerLeaseDetectsReplacedLockFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "owner.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameMTSOwnerFile(path, file); err != nil {
		t.Fatalf("freshly opened owner file must match itself: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sameMTSOwnerFile(path, file); err == nil {
		t.Fatal("a closed lease file must not stat")
	}
	reopened, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sameMTSOwnerFile(path, reopened); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("replaced owner file must be detected: %v", err)
	}
}

func TestMTSOwnerRecordWriteFailsClosedOnReadOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	readOnly, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readOnly.Close() })
	if err := writeMTSOwnerRecord(readOnly); err == nil {
		t.Fatal("writing an owner record through a read-only handle must fail")
	}
}

func TestMTSOwnerBusyErrorReportsHolder(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "owner.lock")
	if err := os.WriteFile(lockPath, []byte(`{"pid":4242,"started_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	busy := mtsOwnerBusyError(lockPath)
	if !errors.Is(busy, ErrMTSStoreBusy) || !strings.Contains(busy.Error(), "4242") {
		t.Fatalf("busy error = %v", busy)
	}
	if pid, ok := readMTSOwnerPID(lockPath); !ok || pid != 4242 {
		t.Fatalf("owner pid = %d, %v", pid, ok)
	}
	if _, ok := readMTSOwnerPID(filepath.Join(directory, "missing.lock")); ok {
		t.Fatal("missing owner file must not report a pid")
	}
	if _, ok := readMTSOwnerPID(directory); ok {
		t.Fatal("directory must not report a pid")
	}
	oversized := filepath.Join(directory, "oversized.lock")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", mtsOwnerRecordLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMTSOwnerPID(oversized); ok {
		t.Fatal("oversized owner file must not report a pid")
	}
	malformed := filepath.Join(directory, "malformed.lock")
	if err := os.WriteFile(malformed, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMTSOwnerPID(malformed); ok {
		t.Fatal("malformed owner file must not report a pid")
	}
	if err := os.WriteFile(malformed, []byte(`{"pid":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMTSOwnerPID(malformed); ok {
		t.Fatal("owner file without a positive pid must not report a pid")
	}
	if busy := mtsOwnerBusyError(malformed); !errors.Is(busy, ErrMTSStoreBusy) {
		t.Fatalf("busy error without a holder = %v", busy)
	}
}
