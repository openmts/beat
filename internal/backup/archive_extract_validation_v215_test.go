package backup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractAllowlistedRejectsOversizedExpansion(t *testing.T) {
	root := t.TempDir()
	files := []*zip.File{
		{FileHeader: zip.FileHeader{Name: manifestEntry, Method: zip.Store,
			UncompressedSize64: uint64(MaximumExpandedBytes) + 1}},
		{FileHeader: zip.FileHeader{Name: sqliteEntry, Method: zip.Store}},
		{FileHeader: zip.FileHeader{Name: metricsEntry, Method: zip.Store}},
		{FileHeader: zip.FileHeader{Name: checksumsEntry, Method: zip.Store}},
	}
	if err := extractAllowlisted(files, root); err == nil {
		t.Fatal("archive whose expanded size exceeds the limit was accepted")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("oversized archive extracted entries: %#v, %v", entries, err)
	}
}

func TestExtractEntryFailsWhenThemeParentIsAFile(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "theme.zip")
	writeZipEntries(t, archive, []zipTestEntry{
		{name: "themes/" + repeatHex('a', 64) + "/assets/app.js", content: "bundle"},
	})
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open theme ZIP: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if err := os.WriteFile(filepath.Join(root, "themes"), []byte("file"), 0o600); err != nil {
		t.Fatalf("occupy themes path: %v", err)
	}
	if err := extractEntry(reader.File[0], root); err == nil {
		t.Fatal("entry with an uncreatable parent directory was extracted")
	}
}

func TestExtractEntryRejectsDeclaredSizeMismatch(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "raw.zip")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create raw ZIP: %v", err)
	}
	writer := zip.NewWriter(file)
	raw, err := writer.CreateRaw(&zip.FileHeader{Name: checksumsEntry, Method: zip.Store,
		UncompressedSize64: 4, CompressedSize64: 9})
	if err != nil {
		t.Fatalf("create raw entry: %v", err)
	}
	if _, err := raw.Write([]byte("123456789")); err != nil {
		t.Fatalf("write raw entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close raw ZIP writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close raw ZIP: %v", err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("reopen raw ZIP: %v", err)
	}
	defer func() { _ = reader.Close() }()
	entry := reader.File[0]
	if entry.UncompressedSize64 != 4 || entry.CompressedSize64 != 9 {
		t.Fatalf("raw sizes = %d/%d", entry.UncompressedSize64, entry.CompressedSize64)
	}
	if err := extractEntry(entry, root); err == nil {
		t.Fatal("entry whose payload exceeds its declared size was extracted")
	}
}

func repeatHex(character byte, count int) string {
	repeated := make([]byte, count)
	for index := range repeated {
		repeated[index] = character
	}
	return string(repeated)
}
