package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThemeCommandPackValidateAndInputErrors(t *testing.T) {
	source := commandSource(t)
	archive := filepath.Join(t.TempDir(), "example.theme.zip")
	var output bytes.Buffer
	for _, args := range [][]string{
		{"beat-theme", "pack", "--source", source, "--output", archive},
		{"beat-theme", "validate", "--archive", archive},
	} {
		if err := newCommand(&output).Run(t.Context(), args); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(output.String(), "sha256") != 2 || !strings.Contains(output.String(), "beat-slate") {
		t.Fatal(output.String())
	}
	for _, args := range [][]string{
		{"beat-theme", "pack", "--source", source},
		{"beat-theme", "pack", "--source", source, "--output", archive, "extra"},
		{"beat-theme", "pack", "--source", filepath.Join(source, "missing"), "--output", archive},
		{"beat-theme", "pack", "--source", source, "--output", source},
		{"beat-theme", "validate", "--archive", archive, "extra"},
		{"beat-theme", "validate", "--archive", filepath.Join(source, "missing")},
		{"beat-theme", "validate", "--archive", source},
	} {
		if err := newCommand(io.Discard).Run(t.Context(), args); err == nil {
			t.Fatalf("accepted invalid args: %q", args)
		}
	}
}

func TestThemeCommandReportsValidationAndOutputErrors(t *testing.T) {
	if err := describeArchive(io.Discard, []byte("not a zip")); err == nil {
		t.Fatal("invalid archive accepted")
	}
	source := commandSource(t)
	archive := filepath.Join(t.TempDir(), "example.zip")
	if err := newCommand(failingWriter{}).Run(t.Context(), []string{"beat-theme", "pack", "--source", source, "--output", archive}); err == nil {
		t.Fatal("output error ignored")
	}
	if err := os.WriteFile(filepath.Join(source, "theme.json"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := newCommand(io.Discard).Run(t.Context(), []string{"beat-theme", "pack", "--source", source, "--output", archive}); err == nil {
		t.Fatal("bad source accepted")
	}
}

func TestThemeCommandExitStatus(t *testing.T) {
	source := commandSource(t)
	var failures bytes.Buffer
	if code := commandExit([]string{"beat-theme", "pack", "--source", source, "--output", filepath.Join(t.TempDir(), "theme.zip")}, io.Discard, &failures); code != 0 || failures.Len() != 0 {
		t.Fatalf("success: %d %s", code, failures.String())
	}
	if code := commandExit([]string{"beat-theme", "validate", "--archive", "missing.zip"}, io.Discard, &failures); code != 1 || !strings.Contains(failures.String(), "theme:") {
		t.Fatalf("failure: %d %s", code, failures.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func commandSource(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"beat-theme.json", "theme.json", "preview.png"} {
		content, err := os.ReadFile(filepath.Join("../../themes/slate", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}
