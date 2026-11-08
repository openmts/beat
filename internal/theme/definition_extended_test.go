package theme

import (
	"encoding/json"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestDefinitionAcceptsExtendedVisualControls(t *testing.T) {
	definition := validDefinition()
	definition.Surface = model.ThemeSurfaceSoft
	definition.Summary = &model.ThemeSummary{Layout: model.ThemeSummaryFocus,
		Metrics: []string{model.ThemeSummaryGroups, model.ThemeSummaryAvailability, model.ThemeSummaryThroughput}}
	definition.Shell = &model.ThemeShell{Background: model.ThemeShellGrid, Header: model.ThemeHeaderTransparent, Footer: model.ThemeFooterInset}
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDefinition(raw)
	if err != nil {
		t.Fatalf("extended controls rejected: %v", err)
	}
	if parsed.Surface != model.ThemeSurfaceSoft || parsed.Summary == nil || parsed.Summary.Layout != model.ThemeSummaryFocus ||
		parsed.Shell == nil || parsed.Shell.Background != model.ThemeShellGrid {
		t.Fatalf("extended controls not preserved: %+v", parsed)
	}
}

func TestDefinitionRejectsInvalidExtendedVisualControls(t *testing.T) {
	for name, mutate := range map[string]func(*model.ThemeDefinition){
		"surface":        func(definition *model.ThemeDefinition) { definition.Surface = "css" },
		"summary layout": func(definition *model.ThemeDefinition) { definition.Summary = &model.ThemeSummary{Layout: "grid"} },
		"summary metric": func(definition *model.ThemeDefinition) {
			definition.Summary = &model.ThemeSummary{Metrics: []string{"latency"}}
		},
		"duplicate metric": func(definition *model.ThemeDefinition) {
			definition.Summary = &model.ThemeSummary{Metrics: []string{"traffic", "traffic"}}
		},
		"too many metrics": func(definition *model.ThemeDefinition) {
			definition.Summary = &model.ThemeSummary{Metrics: []string{"availability", "busiest", "traffic", "throughput", "groups", "availability"}}
		},
		"shell background": func(definition *model.ThemeDefinition) { definition.Shell = &model.ThemeShell{Background: "image"} },
		"shell header":     func(definition *model.ThemeDefinition) { definition.Shell = &model.ThemeShell{Header: "fixed"} },
		"shell footer":     func(definition *model.ThemeDefinition) { definition.Shell = &model.ThemeShell{Footer: "floating"} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := validDefinition()
			mutate(&candidate)
			raw, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseDefinition(raw); err == nil {
				t.Fatal("invalid extended control accepted")
			}
		})
	}
	if _, err := ParseDefinition([]byte(`{"schema_version":1,"overview":"cards","detail":"balanced","density":"comfortable","radius":6,"surface":"solid","summary":{"layout":"cards","script":"bad"},"light":{},"dark":{}}`)); err == nil {
		t.Fatal("unknown nested summary field accepted")
	}
}

func TestThemeConfigurationAppliesExtendedVisualControls(t *testing.T) {
	keys := []string{"surface", "summary_layout", "shell_background", "shell_header", "shell_footer"}
	schema := &model.ThemeConfigurationSchema{Type: model.ThemeConfigurationManaged}
	for _, key := range keys {
		schema.Items = append(schema.Items, model.ThemeConfigurationItem{Key: key})
	}
	configuration, err := ParseThemeConfiguration([]byte(`{"surface":"outline","summary_layout":"strip","shell_background":"spotlight","shell_header":"solid","shell_footer":"minimal"}`), schema)
	if err != nil {
		t.Fatal(err)
	}
	resolved := ApplyThemeConfiguration(validDefinition(), configuration)
	if resolved.Surface != model.ThemeSurfaceOutline || resolved.Summary == nil || resolved.Summary.Layout != model.ThemeSummaryStrip ||
		resolved.Shell == nil || resolved.Shell.Background != model.ThemeShellSpotlight || resolved.Shell.Header != model.ThemeHeaderSolid ||
		resolved.Shell.Footer != model.ThemeFooterMinimal {
		t.Fatalf("extended configuration not applied: %+v", resolved)
	}
	for _, raw := range []string{`{"summary_layout":"grid"}`, `{"shell_background":"image"}`, `{"surface":"glass"}`} {
		if _, err := ParseThemeConfiguration([]byte(raw), schema); err == nil {
			t.Fatalf("invalid extended configuration accepted: %s", raw)
		}
	}
}
