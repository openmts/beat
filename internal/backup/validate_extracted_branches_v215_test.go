package backup

import (
	"archive/zip"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func extractFixtureArchive(t *testing.T, archive string) string {
	t.Helper()
	root := t.TempDir()
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open fixture archive: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if err := extractAllowlisted(reader.File, root); err != nil {
		t.Fatalf("extract fixture archive: %v", err)
	}
	return root
}

func mutateExtractedManifest(t *testing.T, root string, mutate func(*model.BackupManifest)) {
	t.Helper()
	manifest, err := readManifest(filepath.Join(root, manifestEntry))
	if err != nil {
		t.Fatalf("read extracted manifest: %v", err)
	}
	mutate(&manifest)
	if err := writeManifestOnly(root, manifest); err != nil {
		t.Fatalf("write extracted manifest: %v", err)
	}
}

func TestValidateExtractedRejectsInconsistentPayloads(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	archive := filepath.Join(fixture.service.root, record.Filename)
	cases := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"catalog revision mismatch", func(t *testing.T, root string) {
			mutateExtractedManifest(t, root, func(manifest *model.BackupManifest) {
				manifest.CatalogRevision++
			})
		}},
		{"unrepresentable cutoff", func(t *testing.T, root string) {
			mutateExtractedManifest(t, root, func(manifest *model.BackupManifest) {
				manifest.SnapshotCutoff = time.Unix(0, math.MaxInt64).UTC()
			})
		}},
		{"metric row count mismatch", func(t *testing.T, root string) {
			mutateExtractedManifest(t, root, func(manifest *model.BackupManifest) {
				manifest.MetricRows++
			})
		}},
		{"metrics payload unreadable", func(t *testing.T, root string) {
			content := []byte("not a gzip stream")
			if err := os.WriteFile(filepath.Join(root, metricsEntry), content, 0o600); err != nil {
				t.Fatalf("corrupt metrics payload: %v", err)
			}
			mutateExtractedManifest(t, root, func(manifest *model.BackupManifest) {
				manifest.Checksums[metricsEntry] = sha256Hex(content)
				manifest.PayloadSizes[metricsEntry] = int64(len(content))
			})
		}},
		{"checksum list mismatch", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, checksumsEntry),
				[]byte("aa  only.zip\n"), 0o600); err != nil {
				t.Fatalf("rewrite checksum list: %v", err)
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := extractFixtureArchive(t, archive)
			testCase.mutate(t, root)
			if _, err := validateExtracted(t.Context(), root); err == nil {
				t.Fatalf("archive with %s was accepted", testCase.name)
			}
		})
	}
}
