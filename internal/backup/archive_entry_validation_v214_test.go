package backup

import (
	"archive/zip"
	"bytes"
	"testing"
)

func archiveEntryFor(t *testing.T, name string) *zip.File {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return reader.File[0]
}

func TestValidateArchiveEntryRejectsInvalidThemePackagePath(t *testing.T) {
	file := archiveEntryFor(t, "themes/not-a-digest/archive.zip")
	if err := validateArchiveEntry(file, map[string]bool{}); err == nil {
		t.Fatal("theme entry with invalid package digest accepted")
	}
}

func TestSafeArchiveNameRejectsUncleanPaths(t *testing.T) {
	for _, name := range []string{"/absolute/entry", "themes/../escape", "a//b"} {
		if safeArchiveName(name) {
			t.Fatalf("unsafe archive name %q accepted", name)
		}
	}
	if !safeArchiveName(geoIPCurrentEntry) {
		t.Fatal("allowlisted MMDB payload rejected")
	}
}
