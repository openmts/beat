package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

type syntheticDeclarativeTheme struct {
	id, name, version         string
	overview, detail, summary string
	surface, background       string
	header, footer            string
	metrics                   []string
}

// declarativeBrowserThemes 是浏览器门禁的临时包，不进入产品分发包。
var declarativeBrowserThemes = []syntheticDeclarativeTheme{
	{id: "fixture-cards", name: "Fixture Cards", overview: "cards", detail: "balanced", summary: "cards", metrics: []string{"availability", "busiest", "traffic", "throughput"}, surface: "solid", background: "plain", header: "glass", footer: "plain"},
	{id: "fixture-table", name: "Fixture Table", overview: "table", detail: "compact", summary: "strip", metrics: []string{"availability", "busiest", "throughput", "traffic", "groups"}, surface: "outline", background: "grid", header: "solid", footer: "minimal"},
	{id: "fixture-tiles", name: "Fixture Tiles", overview: "tiles", detail: "spotlight", summary: "focus", metrics: []string{"availability", "busiest", "traffic", "throughput"}, surface: "soft", background: "wash", header: "transparent", footer: "plain"},
}

func syntheticDeclarativeArchive(t *testing.T, spec syntheticDeclarativeTheme) []byte {
	t.Helper()
	if spec.version == "" {
		spec.version = "1.0.0"
	}
	definition := map[string]any{}
	if err := json.Unmarshal([]byte(themeFixtureDefinitionJSON), &definition); err != nil {
		t.Fatal(err)
	}
	definition["overview"], definition["detail"] = spec.overview, spec.detail
	definition["surface"] = spec.surface
	definition["summary"] = map[string]any{"layout": spec.summary, "metrics": spec.metrics}
	definition["shell"] = map[string]any{"background": spec.background, "header": spec.header, "footer": spec.footer}
	rawDefinition, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.ThemeManifest{SchemaVersion: 1, ID: spec.id, Name: spec.name, Description: "Browser fixture theme",
		Version: spec.version, Author: "Beat", License: "MIT", Runtime: model.ThemeRuntimeDeclarative,
		MinimumBeatVersion: "0.1.0", Preview: "preview.png"}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	for name, content := range map[string][]byte{"beat-theme.json": rawManifest, "theme.json": rawDefinition, "preview.png": []byte(themeFixturePNG)} {
		if err := os.WriteFile(filepath.Join(source, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := theme.BuildArchive(theme.PackOptions{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	return archive
}
