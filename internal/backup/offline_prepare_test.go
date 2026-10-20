package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRestoreFailureCleansOnlyGeneratedFiles(t *testing.T) {
	for _, phase := range []string{"key-read", "key-inspect", "theme-copy"} {
		t.Run(phase, func(t *testing.T) {
			source, target := t.TempDir(), t.TempDir()
			writeTestFile(t, filepath.Join(source, sqliteEntry), "SQLite snapshot")
			key := filepath.Join(source, filepath.FromSlash(dataKeyEntry))
			if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
				t.Fatal(err)
			}
			switch phase {
			case "key-read":
				if err := os.Mkdir(key, 0o700); err != nil {
					t.Fatal(err)
				}
			case "key-inspect":
				if err := os.Symlink("admin-data.key", key); err != nil {
					t.Fatal(err)
				}
			case "theme-copy":
				writeTestFile(t, key, "snapshot key")
				if err := os.Mkdir(filepath.Join(source, "themes"), 0o700); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(target, "themes"), "prior theme state")
			}
			database := filepath.Join(target, "beat.db")
			writeTestFile(t, database, "prior database")
			writeTestFile(t, filepath.Join(target, "admin-data.key"), "prior key")
			_, err := prepareRestore(&validatedArchive{root: source}, database, filepath.Join(target, "beat_mts"), target, "failure")
			if err == nil {
				t.Fatal("invalid preparation accepted")
			}
			assertFileContents(t, database, []byte("prior database"))
			assertFileContents(t, filepath.Join(target, "admin-data.key"), []byte("prior key"))
			for _, generated := range []string{database + ".restore-new-failure", filepath.Join(target, "admin-data.key.restore-new-failure")} {
				if _, err := os.Lstat(generated); !os.IsNotExist(err) {
					t.Fatalf("private intermediate copy remained: %s", filepath.Base(generated))
				}
			}
		})
	}
}

func TestThemePreparationOwnsItsCleanupScope(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "new-theme-tree")
	if err := copyTree(filepath.Join(root, "missing-source"), destination); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("failed tree copy left a generated directory")
	}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "assets", "theme.css"), "new theme")
	if err := copyTree(source, destination); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, filepath.Join(destination, "assets", "theme.css"), []byte("new theme"))
	writeTestFile(t, filepath.Join(destination, "retained"), "preexisting tree")
	if err := copyTree(source, destination); err == nil {
		t.Fatal("existing destination was reused")
	}
	assertFileContents(t, filepath.Join(destination, "retained"), []byte("preexisting tree"))
}
