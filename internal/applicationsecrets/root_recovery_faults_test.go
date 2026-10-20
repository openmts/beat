package applicationsecrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rootRecoveryFaultCases() map[string]func(*testing.T, string) {
	return map[string]func(*testing.T, string){
		"malformed": func(t *testing.T, dir string) {
			writeRootFault(t, filepath.Join(dir, rotationJournalDir, "journal.json"), []byte("bad"))
		},
		"unknown_field": func(t *testing.T, dir string) {
			alterRootJournal(t, dir, func(j map[string]any) { j["unexpected"] = true })
		},
		"version": func(t *testing.T, dir string) { alterRootJournal(t, dir, func(j map[string]any) { j["version"] = 2 }) },
		"fingerprint": func(t *testing.T, dir string) {
			alterRootJournal(t, dir, func(j map[string]any) { j["previous_fingerprint"] = "bad" })
		},
		"generation": func(t *testing.T, dir string) {
			alterRootJournal(t, dir, func(j map[string]any) { j["previous_generation"] = 8; j["next_generation"] = 9 })
		},
		"candidate_missing": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, rotationJournalDir, "old.key")); err != nil {
				t.Fatal(err)
			}
		},
		"candidate_wrong": func(t *testing.T, dir string) {
			writeRootFault(t, filepath.Join(dir, rotationJournalDir, "old.key"), bytes.Repeat([]byte{8}, rootKeyBytes))
		},
		"current_wrong": func(t *testing.T, dir string) {
			writeRootFault(t, filepath.Join(dir, "admin-data.key"), bytes.Repeat([]byte{9}, rootKeyBytes))
		},
		"current_unsafe": func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, "admin-data.key"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
}

func TestRecoveryRejectsCorruptJournalAndNeverReplacesUnrelatedRoot(t *testing.T) {
	for name, mutate := range rootRecoveryFaultCases() {
		t.Run(name, func(t *testing.T) {
			database, directory := lifecycleTestDB(t)
			service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
			if err != nil {
				t.Fatal(err)
			}
			service.Close()
			config := RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db"), fault: func(phase string) error {
				if phase == "root_journal_published" {
					return errors.New("interrupted")
				}
				return nil
			}}
			if err := rotateRoot(t.Context(), config); err == nil {
				t.Fatal("fault not injected")
			}
			mutate(t, directory)
			before, err := os.ReadFile(filepath.Join(directory, "admin-data.key"))
			if err != nil {
				t.Fatal(err)
			}
			if err := RecoverRootFiles(t.Context(), directory, config.DatabasePath); err == nil {
				t.Fatal("corrupt rotation journal accepted")
			}
			after, err := os.ReadFile(filepath.Join(directory, "admin-data.key"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("unrelated root replaced: %v", err)
			}
		})
	}
}

func TestRootJournalCanRestoreMissingCurrentRootAndResumeCLI(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	config := RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db"), fault: func(phase string) error {
		if phase == "root_db_committed" {
			return errors.New("interrupted")
		}
		return nil
	}}
	if err := rotateRoot(t.Context(), config); err == nil {
		t.Fatal("fault not injected")
	}
	if err := os.Remove(filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
	config.fault = nil
	if err := rotateRoot(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(t.Context(), config.DatabasePath, filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func TestPendingRestoreAndAmbiguousRotationBlockOfflineChanges(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	path := filepath.Join(directory, "restore.pending.json")
	for _, value := range []string{`{"state":"pending"}`, `{}`, `broken`, strings.Repeat("x", 4097)} {
		writeRootFault(t, path, []byte(value))
		if err := rotateRoot(t.Context(), RootRotationConfig{DataDir: directory, DatabasePath: filepath.Join(directory, "beat.db")}); err == nil {
			t.Fatal("pending or corrupt restore accepted")
		}
	}
	writeRootFault(t, path, []byte(`{"state":"applied"}`))
	if err := rejectPendingRestore(directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range rotationDirectories[:2] {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rootRotationMarker(directory); err == nil {
		t.Fatal("ambiguous journals accepted")
	}
	if err := RecoverRootFiles(t.Context(), directory, filepath.Join(directory, "beat.db")); err == nil {
		t.Fatal("ambiguous recovery accepted")
	}
}

func alterRootJournal(t *testing.T, directory string, change func(map[string]any)) {
	t.Helper()
	path := filepath.Join(directory, rotationJournalDir, "journal.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal map[string]any
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	change(journal)
	data, err = json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	writeRootFault(t, path, data)
}

func writeRootFault(t *testing.T, path string, value []byte) {
	t.Helper()
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
}
