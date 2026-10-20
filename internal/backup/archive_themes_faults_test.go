package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

const themeArchiveDigest = "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"

func seedThemeArchivePackage(t *testing.T, root string) {
	t.Helper()
	pkg := filepath.Join(root, "packages", themeArchiveDigest)
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "theme.zip"), []byte("theme"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestThemeArchiveCopyFaults 覆盖主题包归档时缺失目录、非法条目、目标不可写与遍历失败。
func TestThemeArchiveCopyFaults(t *testing.T) {
	t.Run("missing packages directory", func(t *testing.T) {
		service := &Service{themesDir: filepath.Join(t.TempDir(), "themes")}
		copied, err := service.copyThemes(t.TempDir(), map[string]model.BackupThemeFile{})
		if err != nil || len(copied) != 0 {
			t.Fatalf("missing theme packages = %v/%v", copied, err)
		}
	})
	t.Run("non package entries skipped", func(t *testing.T) {
		root := t.TempDir()
		packages := filepath.Join(root, "packages")
		if err := os.MkdirAll(filepath.Join(packages, "not-a-digest"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packages, "stray.txt"), []byte("stray"), 0o600); err != nil {
			t.Fatal(err)
		}
		files := map[string]model.BackupThemeFile{}
		copied, err := (&Service{themesDir: root}).copyThemes(t.TempDir(), files)
		if err != nil || len(copied) != 0 || len(files) != 0 {
			t.Fatalf("stray theme entries = %v/%v/%d", copied, err, len(files))
		}
	})
	t.Run("package copied", func(t *testing.T) {
		root := t.TempDir()
		seedThemeArchivePackage(t, root)
		files := map[string]model.BackupThemeFile{}
		copied, err := (&Service{themesDir: root}).copyThemes(t.TempDir(), files)
		if err != nil || len(copied) != 1 || len(files) != 1 {
			t.Fatalf("theme package copy = %v/%v/%d", copied, err, len(files))
		}
	})
	t.Run("target not creatable", func(t *testing.T) {
		root := t.TempDir()
		seedThemeArchivePackage(t, root)
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (&Service{themesDir: root}).copyThemes(filepath.Join(blocker, "nested"), map[string]model.BackupThemeFile{}); err == nil {
			t.Fatal("unwritable theme backup target was accepted")
		}
	})
	t.Run("symlink skipped", func(t *testing.T) {
		root := t.TempDir()
		seedThemeArchivePackage(t, root)
		if err := os.Symlink(filepath.Join(root, "packages", themeArchiveDigest, "theme.zip"),
			filepath.Join(root, "packages", themeArchiveDigest, "link.zip")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		files := map[string]model.BackupThemeFile{}
		copied, err := (&Service{themesDir: root}).copyThemes(t.TempDir(), files)
		if err != nil || len(copied) != 1 {
			t.Fatalf("symlink theme entry = %v/%v", copied, err)
		}
	})
}

// TestThemeArchiveTreeFaults 覆盖主题目录遍历与私有文件写入的失败分支。
func TestThemeArchiveTreeFaults(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		request := themeArchiveCopy{source: filepath.Join(t.TempDir(), "absent"),
			target: filepath.Join(t.TempDir(), "target"), digest: themeArchiveDigest,
			files: map[string]model.BackupThemeFile{}}
		if _, err := copyThemeArchiveTree(request); err == nil {
			t.Fatal("missing theme source was accepted")
		}
	})
	t.Run("existing target file", func(t *testing.T) {
		source := t.TempDir()
		if err := os.WriteFile(filepath.Join(source, "theme.zip"), []byte("theme"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "theme.zip"), []byte("existing"), 0o600); err != nil {
			t.Fatal(err)
		}
		request := themeArchiveCopy{source: source, target: target, digest: themeArchiveDigest,
			files: map[string]model.BackupThemeFile{}}
		if _, err := copyThemeArchiveTree(request); err == nil {
			t.Fatal("existing theme target file was overwritten")
		}
	})
	t.Run("private file without parent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "absent", "file")
		if err := writePrivateFile(path, []byte("x")); err == nil {
			t.Fatal("dangling private file path was accepted")
		}
	})
}

// TestRestoreStateHelpers 覆盖审计期望状态与回滚目标的兜底分支。
func TestRestoreStateHelpers(t *testing.T) {
	if got := firstExpectedState(nil); got != "" {
		t.Fatalf("empty expected state = %q", got)
	}
	if got := firstExpectedState([]string{model.BackupStateStaged}); got != model.BackupStateStaged {
		t.Fatalf("expected state = %q", got)
	}
	if got := previousRestoreState("unexpected"); got != model.BackupStateValidated {
		t.Fatalf("unknown rollback target = %q", got)
	}
	for _, state := range []string{model.BackupStateReady, model.BackupStateValidated} {
		if got := previousRestoreState(state); got != state {
			t.Fatalf("rollback target %s = %q", state, got)
		}
	}
	if !strings.Contains(backupTransitionDetail("a", "b", "gen"), `"generation":"gen"`) {
		t.Fatal("transition detail lost the restore generation")
	}
}
