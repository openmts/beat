package backup

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func TestRestoreJournalValidationErrors(t *testing.T) {
	root := t.TempDir()
	invalidJSON := filepath.Join(root, "invalid.json")
	writeTestFile(t, invalidJSON, "{")
	if _, err := readJournal(invalidJSON); err == nil {
		t.Fatal("invalid restore journal decoded")
	}
	empty := filepath.Join(root, "empty.json")
	writeTestFile(t, empty, `{}`)
	if _, err := readJournal(empty); err == nil {
		t.Fatal("incomplete restore journal accepted")
	}
	parentFile := filepath.Join(root, "parent")
	writeTestFile(t, parentFile, "file")
	if err := writeJournal(filepath.Join(parentFile, "journal.json"), restoreJournal("id", "archive")); err == nil {
		t.Fatal("restore journal created below regular file")
	}
	directoryPath := filepath.Join(root, "journal-directory")
	if err := os.Mkdir(directoryPath, 0o700); err != nil {
		t.Fatalf("create journal directory: %v", err)
	}
	if err := writeJournal(directoryPath, restoreJournal("id", "archive")); err == nil {
		t.Fatal("restore journal replaced a directory")
	}
	if journalTimestamp(time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)) != "20260102T030405.000000006Z" {
		t.Fatal("restore journal timestamp format is incorrect")
	}
}

func TestApplyPendingReadAndArchiveErrors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "restore.pending.json"), "{")
	if err := ApplyPending(t.Context(), root, filepath.Join(root, "db"), filepath.Join(root, "mts")); err == nil {
		t.Fatal("invalid pending restore journal accepted")
	}
	archivePath := filepath.Join(root, "backups", "missing.zip")
	if err := os.Mkdir(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatalf("create backups directory: %v", err)
	}
	if err := writeJournal(filepath.Join(root, "restore.pending.json"), restoreJournal("id", archivePath)); err != nil {
		t.Fatalf("write missing-archive journal: %v", err)
	}
	if err := ApplyPending(t.Context(), root, filepath.Join(root, "db"), filepath.Join(root, "mts")); err == nil {
		t.Fatal("pending restore with missing archive applied")
	}
}

func TestArchiveContainerValidationErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := validateArchive(t.Context(), filepath.Join(root, "missing.zip"), root); err == nil {
		t.Fatal("missing archive validated")
	}
	if _, err := validateArchive(t.Context(), root, root); err == nil {
		t.Fatal("directory validated as archive")
	}
	notZip := filepath.Join(root, "plain.zip")
	writeTestFile(t, notZip, "plain")
	if _, err := validateArchive(t.Context(), notZip, root); err == nil {
		t.Fatal("plain file validated as ZIP")
	}
	archive := filepath.Join(root, "entries.zip")
	writeZipEntries(t, archive, []zipTestEntry{{name: manifestEntry, content: "{}"}})
	if err := validateArchiveEntries(archive, root); err == nil {
		t.Fatal("archive with too few entries accepted")
	}
	unsupported := zip.FileHeader{Name: manifestEntry, Method: 99}
	if err := extractAllowlisted(fakeZipFiles(unsupported), root); err == nil {
		t.Fatal("unsupported ZIP compression accepted")
	}
	header := zip.FileHeader{Name: manifestEntry, Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o600)
	if err := extractAllowlisted(fakeZipFiles(header), root); err == nil {
		t.Fatal("symlink ZIP entry accepted")
	}
}

func TestPayloadAndMetricValidationErrors(t *testing.T) {
	root := t.TempDir()
	manifest := model.BackupManifest{Checksums: map[string]string{"unknown": "sum"}, PayloadSizes: map[string]int64{}}
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("unknown manifest payload accepted")
	}
	manifest.Checksums = map[string]string{sqliteEntry: "sum"}
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("inconsistent payload metadata accepted")
	}
	largeChecksums := filepath.Join(root, checksumsEntry)
	if err := os.WriteFile(largeChecksums, bytes.Repeat([]byte{'x'}, 64<<10+1), 0o600); err != nil {
		t.Fatalf("write large checksum list: %v", err)
	}
	if err := validateChecksumList(root, model.BackupManifest{}); err == nil {
		t.Fatal("oversized checksum list accepted")
	}
	plainMetrics := filepath.Join(root, "plain.metrics")
	writeTestFile(t, plainMetrics, "plain")
	if _, err := validateMetrics(plainMetrics, time.Now().UnixNano()); err == nil {
		t.Fatal("uncompressed metrics accepted")
	}
	trailing := filepath.Join(root, "trailing.metrics.gz")
	writeMetricLines(t, trailing, []string{`{"measurement":"cpu"} {}`})
	if _, err := validateMetrics(trailing, time.Now().UnixNano()); err == nil {
		t.Fatal("metric row with trailing data accepted")
	}
	if exactKeys(map[string]string{"node": ""}, "node") {
		t.Fatal("empty metric tag accepted")
	}
	if exactFields(map[string]mts.FieldValue{"value": mts.Int64Value(1)},
		map[string]mts.FieldType{"value": mts.FieldFloat64}) {
		t.Fatal("wrong metric field type accepted")
	}
}

func generatedArchive(t *testing.T, fixture *backupFixture) ([]byte, model.BackupRecord) {
	t.Helper()
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start generated backup: %v", err)
	}
	fixture.service.Wait()
	content, err := os.ReadFile(filepath.Join(fixture.dataDir, "backups", record.Filename))
	if err != nil {
		t.Fatalf("read generated backup: %v", err)
	}
	return content, record
}

func validateArchiveEntries(path, root string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	return extractAllowlisted(reader.File, root)
}

func fakeZipFiles(first zip.FileHeader) []*zip.File {
	headers := []zip.FileHeader{first,
		{Name: sqliteEntry, Method: zip.Store},
		{Name: metricsEntry, Method: zip.Store},
		{Name: checksumsEntry, Method: zip.Store},
	}
	files := make([]*zip.File, 0, len(headers))
	for _, header := range headers {
		files = append(files, &zip.File{FileHeader: header})
	}
	return files
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

var _ io.Reader = failingReader{}
