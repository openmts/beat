package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scaffoldOptions(t *testing.T, name string) initOptions {
	t.Helper()
	return initOptions{name: name, output: filepath.Join(t.TempDir(), "theme"), overview: "cards", detail: "balanced", density: "comfortable", accent: "blue", radius: 6}
}

func TestInitScaffoldPacksCleanlyForEveryAccent(t *testing.T) {
	for _, accent := range accentNames {
		options := scaffoldOptions(t, "Night Ops")
		options.accent = accent
		options.overview = "list"
		options.density = "compact"
		options.font = "mono"
		options.radius = 4
		var output bytes.Buffer
		if err := runInit(options, &output); err != nil {
			t.Fatalf("accent %s: %v", accent, err)
		}
		if !strings.HasPrefix(output.String(), "beat-night-ops 1.0.0 (declarative), 3 files, sha256 ") {
			t.Fatalf("accent %s reported %q", accent, output.String())
		}
		for _, name := range []string{initThemeFile, initManifestFile, initLicenseFile, initPreviewFile} {
			info, err := os.Stat(filepath.Join(options.output, name))
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("accent %s file %s: %v %v", accent, name, info, err)
			}
		}
		info, err := os.Stat(options.output)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("accent %s directory mode: %v %v", accent, info, err)
		}
	}
}

func TestInitScaffoldRejectsUnknownOptions(t *testing.T) {
	cases := map[string]func(*initOptions){
		"accent":   func(options *initOptions) { options.accent = "magenta" },
		"overview": func(options *initOptions) { options.overview = "grid" },
		"detail":   func(options *initOptions) { options.detail = "wide" },
		"density":  func(options *initOptions) { options.density = "airy" },
		"radius":   func(options *initOptions) { options.radius = 9 },
		"font":     func(options *initOptions) { options.font = "comic" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			options := scaffoldOptions(t, "Night Ops")
			mutate(&options)
			if err := runInit(options, &bytes.Buffer{}); err == nil {
				t.Fatal("invalid scaffold option accepted")
			}
			if _, err := os.Stat(options.output); !os.IsNotExist(err) {
				t.Fatal("invalid scaffold still wrote files")
			}
		})
	}
	if err := runInit(initOptions{name: "  ", output: t.TempDir()}, &bytes.Buffer{}); err == nil {
		t.Fatal("blank name accepted")
	}
}

func TestInitScaffoldGuardsExistingDirectories(t *testing.T) {
	options := scaffoldOptions(t, "Repeatable")
	if err := runInit(options, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := runInit(options, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("existing directory overwritten: %v", err)
	}
	options.force = true
	if err := runInit(options, &bytes.Buffer{}); err != nil {
		t.Fatalf("--force rejected: %v", err)
	}
	options.id = "beat-custom"
	options.force = true
	if err := runInit(options, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(options.output, initManifestFile))
	if err != nil || !strings.Contains(string(manifest), `"id": "beat-custom"`) {
		t.Fatalf("explicit id not honoured: %s %v", manifest, err)
	}
}

func TestInitSlug(t *testing.T) {
	cases := map[string]string{"Night Ops": "night-ops", "  Fleet--View  ": "fleet-view", "已部署": "theme", "A": "a"}
	for name, expected := range cases {
		if actual := initSlug(name); actual != expected {
			t.Fatalf("slug of %q is %q, want %q", name, actual, expected)
		}
	}
	long := initSlug(strings.Repeat("Long ", 12))
	if len(long) > 40 || !strings.HasPrefix(long, "long-long") || strings.HasSuffix(long, "-") {
		t.Fatalf("long slug not bounded: %q", long)
	}
}

func TestInitCommandWiring(t *testing.T) {
	output, failures := &bytes.Buffer{}, &bytes.Buffer{}
	directory := filepath.Join(t.TempDir(), "cli-theme")
	if code := commandExit([]string{"beat-theme", "init", "--name", "CLI Theme", "--output", directory}, output, failures); code != 0 {
		t.Fatalf("init exit %d: %s", code, failures.String())
	}
	if !strings.Contains(output.String(), "beat-cli-theme 1.0.0 (declarative), 3 files") {
		t.Fatalf("init output %q", output.String())
	}
	if code := commandExit([]string{"beat-theme", "init", "--name", "CLI Theme"}, &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
		t.Fatal("missing --output accepted")
	}
	if code := commandExit([]string{"beat-theme", "init", "--name", "CLI Theme", "--output", directory, "extra"}, &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
		t.Fatal("positional argument accepted")
	}
}
