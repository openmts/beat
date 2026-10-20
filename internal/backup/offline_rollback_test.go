package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreserveOldRestoresEarlierTargetsAfterInspectionError(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	obstacle := filepath.Join(root, "not-directory")
	writeTestFile(t, live, "original database")
	writeTestFile(t, obstacle, "file")
	_, _, err := NewApplier().preserveOld([]restoreTarget{{live: live}, {live: filepath.Join(obstacle, "child")}}, "test")
	if err == nil {
		t.Fatal("inspection error ignored")
	}
	content, readErr := os.ReadFile(live)
	if readErr != nil || string(content) != "original database" {
		t.Fatal("earlier live target not restored after inspection failure")
	}
}

func TestActivationFailuresRestoreDatabaseAndMetrics(t *testing.T) {
	for _, phase := range []string{"preserve", "activate", "metrics"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			live, newPath := filepath.Join(root, "beat.db"), filepath.Join(root, "new.db")
			metrics := filepath.Join(root, "metrics")
			if err := os.Mkdir(metrics, 0o700); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, live, "original database")
			writeTestFile(t, newPath, "restored database")
			writeTestFile(t, filepath.Join(metrics, "original"), "original metrics")
			invalid := filepath.Join(root, "metrics.gz")
			writeTestFile(t, invalid, "damaged gzip")
			prepared := &preparedRestore{targets: []restoreTarget{{live: live, new: newPath}}, mtsLive: metrics, metricsPath: invalid}
			applier := NewApplier()
			applier.rename = func(source, destination string) error {
				if phase == "preserve" && source == live {
					return errors.New("preserve unavailable")
				}
				if phase == "activate" && source == newPath {
					return errors.New("activation unavailable")
				}
				return os.Rename(source, destination)
			}
			if _, err := applier.activate(t.Context(), prepared, "rollback-test"); err == nil {
				t.Fatal("failed restore reported success")
			}
			assertFileContents(t, live, []byte("original database"))
			assertFileContents(t, filepath.Join(metrics, "original"), []byte("original metrics"))
		})
	}
}

func TestApplyPendingRollsBackWhenActivationOrJournalPublicationFails(t *testing.T) {
	fixture := newBackupFixture(t)
	_ = seedRestoreSession(t, fixture)
	for _, phase := range []string{"activation", "journal"} {
		t.Run(phase, func(t *testing.T) {
			target := stageAuthSnapshot(t, fixture)
			live := filepath.Join(target, "beat.db")
			metrics := filepath.Join(target, "beat_mts")
			if err := os.Mkdir(metrics, 0o700); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, live, "prior database")
			writeTestFile(t, filepath.Join(target, "admin-data.key"), "prior key")
			writeTestFile(t, filepath.Join(metrics, "marker"), "prior metrics")
			applier := NewApplier()
			applier.rename = func(source, destination string) error {
				activating := strings.Contains(source, ".restore-new-")
				if activating && destination == live && phase == "activation" {
					return errors.New("activation refused")
				}
				if err := os.Rename(source, destination); err != nil {
					return err
				}
				if activating && destination == filepath.Join(target, "admin-data.key") && phase == "journal" {
					journal := filepath.Join(target, "restore.pending.json")
					if err := os.Rename(journal, journal+".retained"); err != nil {
						return err
					}
					return os.Mkdir(journal, 0o700)
				}
				return nil
			}
			if err := applier.ApplyPending(t.Context(), target, live, metrics); err == nil {
				t.Fatal("failed publication reported success")
			}
			assertFileContents(t, live, []byte("prior database"))
			assertFileContents(t, filepath.Join(target, "admin-data.key"), []byte("prior key"))
			assertFileContents(t, filepath.Join(metrics, "marker"), []byte("prior metrics"))
		})
	}
}

func assertFileContents(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(expected, actual) {
		t.Fatalf("original state not preserved at %s: %v", filepath.Base(path), err)
	}
}
