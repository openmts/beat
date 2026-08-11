package backup

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func TestExtractAllowlistedMissingRequiredEntry(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "missing-required.zip")
	writeZipEntries(t, archive, []zipTestEntry{
		{name: manifestEntry, content: "manifest"},
		{name: sqliteEntry, content: "sqlite"},
		{name: metricsEntry, content: "metrics"},
		{name: dataKeyEntry, content: "key"},
	})
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open missing-required ZIP: %v", err)
	}
	defer func() { _ = reader.Close() }()
	output := filepath.Join(root, "output")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatalf("create extraction output: %v", err)
	}
	if err := extractAllowlisted(reader.File, output); err == nil {
		t.Fatal("archive missing checksum list accepted")
	}
}

func TestExtractEntryDestinationAndSizeErrors(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "entry.zip")
	writeZipEntries(t, archive, []zipTestEntry{{name: manifestEntry, content: "content"}})
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open entry ZIP: %v", err)
	}
	defer func() { _ = reader.Close() }()
	output := filepath.Join(root, "output")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatalf("create extraction output: %v", err)
	}
	writeTestFile(t, filepath.Join(output, manifestEntry), "existing")
	if err := extractEntry(reader.File[0], output); err == nil {
		t.Fatal("ZIP extraction replaced existing file")
	}
	if err := os.Remove(filepath.Join(output, manifestEntry)); err != nil {
		t.Fatalf("remove existing extraction target: %v", err)
	}
	reader.File[0].UncompressedSize64++
	if err := extractEntry(reader.File[0], output); err == nil {
		t.Fatal("ZIP entry with mismatched declared size accepted")
	}
}

func TestArchiveMetadataAndWriterFailures(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, checksumsEntry), 0o700); err != nil {
		t.Fatalf("create checksum directory: %v", err)
	}
	manifest := model.BackupManifest{Checksums: map[string]string{}, PayloadSizes: map[string]int64{}}
	if err := writeMetadataFiles(root, manifest); err == nil {
		t.Fatal("metadata replaced checksum directory")
	}
	payload := filepath.Join(root, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		t.Fatalf("create directory payload: %v", err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if err := addArchiveEntries(writer, root, []string{"payload"}); err == nil {
		t.Fatal("directory ZIP payload was copied")
	}
	_ = writer.Close()
}

func TestCopyDataKeyDestinationFailures(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "admin-data.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{1}, 32), 0o600); err != nil {
		t.Fatalf("write administrator key: %v", err)
	}
	service := &Service{dataKeyPath: keyPath}
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "secrets"), "file")
	if err := service.copyDataKey(workspace); err == nil {
		t.Fatal("key directory replaced a regular file")
	}
	workspace = t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "secrets"), 0o700); err != nil {
		t.Fatalf("create key directory: %v", err)
	}
	writeTestFile(t, filepath.Join(workspace, dataKeyEntry), "existing")
	if err := service.copyDataKey(workspace); err == nil {
		t.Fatal("existing administrator key was replaced")
	}
}

func TestManifestAndChecksumParsingFailures(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, manifestEntry), "{")
	if _, err := readManifest(filepath.Join(root, manifestEntry)); err == nil {
		t.Fatal("invalid manifest JSON accepted")
	}
	manifest := model.BackupManifest{Checksums: map[string]string{sqliteEntry: "right"}}
	writeTestFile(t, filepath.Join(root, checksumsEntry), "one  platform.sqlite\ntwo  metrics.jsonl.gz\n")
	if err := validateChecksumList(root, manifest); err == nil {
		t.Fatal("checksum list with extra entry accepted")
	}
	writeTestFileReplace(t, filepath.Join(root, checksumsEntry), "wrong  platform.sqlite\n")
	if err := validateChecksumList(root, manifest); err == nil {
		t.Fatal("incorrect checksum value accepted")
	}
}

func TestMetricValidationTrailingDataAndKnownNames(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	record := metricRecord{Measurement: "cpu", Tags: map[string]string{"node": "node"},
		TimestampNS: now.Add(-time.Second).UnixNano(),
		Fields:      map[string]mts.FieldValue{"value": mts.Float64Value(1)}}
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal metric record: %v", err)
	}
	path := filepath.Join(root, "trailing.jsonl.gz")
	writeMetricLines(t, path, []string{string(content) + " {}"})
	if _, err := validateMetrics(path, now.UnixNano()); err == nil {
		t.Fatal("valid metric with trailing JSON accepted")
	}
	if knownMetric("definitely_unknown") {
		t.Fatal("unknown metric name accepted")
	}
}

func TestKnownMetricCoversNodeAndDerivedValueMeasurements(t *testing.T) {
	measurements := append(model.MetricNames(), "net_recv_delta", "net_sent_delta")
	for _, measurement := range measurements {
		if !knownMetric(measurement) {
			t.Fatalf("backup validation does not recognize MTS measurement %q", measurement)
		}
	}
	if knownMetric("network_probe") {
		t.Fatal("network_probe must use its typed-field validation path")
	}
}

func writeTestFileReplace(t *testing.T, path, content string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	writeTestFile(t, path, content)
}
