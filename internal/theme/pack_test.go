package theme

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func packSource(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	manifest := model.ThemeManifest{SchemaVersion: 1, ID: "pack-test", Name: "Pack test", Description: "Source bundle", Version: "1.0.0", Author: "Beat", License: "MIT", Runtime: model.ThemeRuntimeDeclarative, MinimumBeatVersion: "0.1.0", Preview: "preview.png"}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{manifestName: raw, definitionName: []byte(fixtureDefinitionJSON), "preview.png": []byte(fixturePNG)} {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestPackUsesProductionValidationAndExcludesItsOutput(t *testing.T) {
	source := packSource(t)
	options := PackOptions{Source: source, Output: filepath.Join(source, "theme.zip")}
	first, err := BuildArchive(options)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateArchive(first)
	if err != nil || len(validated.Manifest.Files) != 2 {
		t.Fatalf("package: %+v %v", validated, err)
	}
	if err := WriteArchive(options.Output, first); err != nil {
		t.Fatal(err)
	}
	second, err := BuildArchive(options)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("package is not deterministic: %v", err)
	}
	info, err := os.Stat(options.Output)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode: %v %v", info, err)
	}
}

func TestPackRejectsUnsafeSourceAndInvalidDefinition(t *testing.T) {
	source := packSource(t)
	if err := os.WriteFile(filepath.Join(source, definitionName), []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
		t.Fatal("invalid definition was packaged")
	}
	source = packSource(t)
	if err := os.Symlink(filepath.Join(source, "preview.png"), filepath.Join(source, "linked.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildArchive(PackOptions{Source: source}); err == nil {
		t.Fatal("symlink was packaged")
	}
}
