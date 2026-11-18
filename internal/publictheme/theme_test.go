package publictheme

import (
	"encoding/json"
	"testing"
)

func TestDefaultDocumentIsValid(t *testing.T) {
	doc := DefaultDocument()
	normalized, err := Normalize(doc)
	if err != nil {
		t.Fatalf("normalize default theme: %v", err)
	}
	if normalized.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected schema version: %d", normalized.SchemaVersion)
	}
	if len(normalized.Home.Blocks) == 0 || len(normalized.NodeDetail.Blocks) == 0 {
		t.Fatal("default theme must include home and node detail blocks")
	}
	if normalized.PresetID != "minimal" {
		t.Fatalf("unexpected preset id: %q", normalized.PresetID)
	}
	if normalized.Home.Blocks[0].Type != "group-filter" || normalized.Home.Blocks[1].Type != "summary-stats" {
		t.Fatalf("default home blocks must lead with group navigation before summary stats: %+v", normalized.Home.Blocks[:2])
	}
}

func TestPresetCatalogContainsDefault(t *testing.T) {
	presets := Presets()
	if len(presets) != 3 {
		t.Fatalf("expected three built-in presets, got %d", len(presets))
	}
	preset, ok := PresetByID("minimal")
	if !ok || !preset.IsDefault {
		t.Fatal("expected minimal preset")
	}
	if preset.Document.PresetID != preset.ID {
		t.Fatalf("preset document id mismatch: %q", preset.Document.PresetID)
	}
	if preset.Document.NodeDetail.Blocks[0].Props == nil {
		t.Fatal("preset documents must be normalized before being exposed")
	}
	classic, ok := PresetByID("beat-default")
	if !ok || classic.IsDefault {
		t.Fatal("expected beat-default preset to remain available and non-default")
	}
	if classic.Document.PresetID != classic.ID || classic.Document.Appearance.Surface != "gradient" {
		t.Fatal("expected the original Beat theme document to be preserved")
	}
	paper, ok := PresetByID("paper")
	if !ok || paper.IsDefault {
		t.Fatal("expected paper preset to be available and non-default")
	}
	if paper.Document.PresetID != paper.ID || len(paper.Document.Home.Blocks) != 1 || paper.Document.Home.Blocks[0].Type != "paper-monitor" {
		t.Fatal("expected paper preset to use the dedicated monitor home block")
	}
}

func TestBeatDefaultHomeCardsShareTheSameWidth(t *testing.T) {
	doc, err := Normalize(BeatDefaultDocument())
	if err != nil {
		t.Fatalf("normalize beat default theme: %v", err)
	}
	for _, block := range doc.Home.Blocks {
		if block.Type == "hero" || block.Type == "group-filter" || block.Type == "summary-stats" || block.Type == "node-collection" {
			if block.Layout.Padding != "none" {
				t.Fatalf("beat default %s block must render at the full content width, got padding %q", block.Type, block.Layout.Padding)
			}
		}
	}
}

func TestParseRejectsUnknownBlockType(t *testing.T) {
	doc := DefaultDocument()
	doc.Home.Blocks[0].Type = "raw-html"
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected unsupported block type to be rejected")
	}
}

func TestParseRejectsDuplicateBlockID(t *testing.T) {
	doc := DefaultDocument()
	doc.NodeDetail.Blocks[1].ID = doc.NodeDetail.Blocks[0].ID
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected duplicate block ids to be rejected")
	}
}

func TestParseRejectsInvalidColor(t *testing.T) {
	doc := DefaultDocument()
	doc.Appearance.Primary = "blue"
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected invalid color to be rejected")
	}
}
