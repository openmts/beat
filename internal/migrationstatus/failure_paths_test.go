package migrationstatus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateKeepsOnlyTheBound(t *testing.T) {
	if got := truncate("abc", 3); got != "abc" {
		t.Fatalf("short value was changed: %q", got)
	}
	if got := truncate("abcd", 3); got != "abc" {
		t.Fatalf("long value was not truncated: %q", got)
	}
}

func TestWriterRejectsMarkersThatBreakTheContract(t *testing.T) {
	directory := privateTemp(t)
	if err := writeMarker(directory, Marker{State: "unknown"}); err == nil {
		t.Fatal("unknown state was written")
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatalf("rejected marker left files behind: %v %v", entries, err)
	}
}

// rename 目标被目录占用时发布失败，且临时文件必须被清掉。
func TestPublishRemovesTemporaryFileWhenRenameFails(t *testing.T) {
	directory := privateTemp(t)
	if err := os.Mkdir(filepath.Join(directory, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := publish(directory, []byte(`{"state":"idle"}`)); err == nil {
		t.Fatal("publish over a directory succeeded")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".migration-status-") {
			t.Fatalf("temporary marker left behind: %s", entry.Name())
		}
	}
}

func TestSyncDirectoryAndReadReportIOFailures(t *testing.T) {
	directory := privateTemp(t)
	if err := syncDirectory(filepath.Join(directory, "absent")); err == nil {
		t.Fatal("sync of a missing directory succeeded")
	}
	file := filepath.Join(directory, "plain-file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(file); err == nil || errors.Is(err, ErrMarkerAbsent) {
		t.Fatalf("read below a regular file reported %v", err)
	}
}
