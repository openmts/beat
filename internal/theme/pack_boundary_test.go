package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackCannotReplaceSourceManifest(t *testing.T) {
	source := packSource(t)
	if _, err := BuildArchive(PackOptions{Source: source, Output: filepath.Join(source, manifestName)}); err == nil {
		t.Fatal("output may overwrite source manifest")
	}
}

func TestPackRejectsMissingHiddenOversizedAndSpecialFiles(t *testing.T) {
	if _, err := BuildArchive(PackOptions{Source: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("missing source accepted")
	}
	for _, name := range []string{".secret", "unknown.txt", "nested/.token.json"} {
		t.Run(name, func(t *testing.T) {
			source := packSource(t)
			path := filepath.Join(source, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
				t.Fatal("unsafe file packaged")
			}
		})
	}
	source := packSource(t)
	if err := os.WriteFile(filepath.Join(source, "large.css"), []byte(strings.Repeat("a", maxAssetFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
		t.Fatal("oversized asset packaged")
	}
}

func TestWriteArchiveRejectsUnsafeDestinationsAndKeepsOriginal(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.zip")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link.zip")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, link, filepath.Join(target, "child.zip")} {
		if err := WriteArchive(path, []byte("new")); err == nil {
			t.Fatalf("unsafe destination accepted: %s", path)
		}
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "original" {
		t.Fatalf("original changed: %q %v", content, err)
	}
	if err := WriteArchive(filepath.Join(directory, "new", "archive.zip"), []byte("safe")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(directory, "new"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode: %v %v", info, err)
	}
}

func TestPackManifestBoundariesAndInventoryLimit(t *testing.T) {
	for _, metadata := range []string{"invalid", strings.Repeat(" ", maxManifestBytes+1)} {
		source := packSource(t)
		if err := os.WriteFile(filepath.Join(source, manifestName), []byte(metadata), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
			t.Fatal("invalid metadata packaged")
		}
	}
	empty := t.TempDir()
	if _, err := BuildArchive(PackOptions{Source: empty}); err == nil {
		t.Fatal("missing manifest accepted")
	}
	if err := os.Mkdir(filepath.Join(empty, manifestName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildArchive(PackOptions{Source: empty}); err == nil {
		t.Fatal("directory manifest accepted")
	}
	source := packSource(t)
	for index := 0; index < maxFiles; index++ {
		name := filepath.Join(source, fmt.Sprintf("asset-%04d.json", index))
		if err := os.WriteFile(name, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
		t.Fatal("unbounded inventory accepted")
	}
}

func TestBundledThemesUseProductionRulesAndDistinctLayouts(t *testing.T) {
	layouts := map[string]bool{}
	for _, name := range []string{"slate", "operations", "canvas"} {
		raw, err := BuildArchive(PackOptions{Source: filepath.Join("../../themes", name)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		archive, err := ValidateArchive(raw)
		if err != nil {
			t.Fatal(err)
		}
		definition, err := ParseDefinition(archive.Content[definitionName])
		if err != nil || layouts[definition.Overview] {
			t.Fatalf("invalid/repeated layout %s: %v", name, err)
		}
		layouts[definition.Overview] = true
	}
	if len(layouts) != 3 {
		t.Fatal(layouts)
	}
	if _, err := colorLuminance("#gggggg"); err == nil {
		t.Fatal("invalid hex accepted")
	}
}

func TestPackRejectsLinkedManifestAndCleansFailedWrites(t *testing.T) {
	source := t.TempDir()
	metadata := filepath.Join(packSource(t), manifestName)
	if err := os.Symlink(metadata, filepath.Join(source, manifestName)); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
		t.Fatal("linked manifest accepted")
	}
	t.Chdir(t.TempDir())
	if err := WriteArchive("", []byte("incomplete")); err == nil {
		t.Fatal("empty destination accepted")
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary output leaked: %v %v", entries, err)
	}
}
