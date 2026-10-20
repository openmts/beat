package theme

import (
	"encoding/json"
	"testing"

	"github.com/beat/backend/internal/model"
)

func validDefinition() model.ThemeDefinition {
	light := map[string]string{}
	for _, token := range PaletteTokens() {
		light[token] = "#172033"
	}
	for _, token := range []string{"background", "card", "popover", "secondary", "muted", "accent"} {
		light[token] = "#ffffff"
	}
	for _, token := range []string{"primary-foreground", "destructive-foreground"} {
		light[token] = "#ffffff"
	}
	dark := map[string]string{}
	for key, value := range light {
		dark[key] = value
	}
	return model.ThemeDefinition{SchemaVersion: 1, Overview: "cards", Detail: "balanced", Density: "comfortable", Radius: 6, Light: light, Dark: dark}
}

func TestDefinitionStrictFieldsAndLayouts(t *testing.T) {
	definition := validDefinition()
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDefinition(raw)
	if err != nil || parsed.Overview != "cards" {
		t.Fatalf("definition: %+v %v", parsed, err)
	}
	for _, mutate := range []func(*model.ThemeDefinition){
		func(d *model.ThemeDefinition) { d.SchemaVersion = 2 }, func(d *model.ThemeDefinition) { d.Overview = "html" },
		func(d *model.ThemeDefinition) { d.Detail = "unknown" }, func(d *model.ThemeDefinition) { d.Density = "custom-class" },
		func(d *model.ThemeDefinition) { d.Radius = 9 }, func(d *model.ThemeDefinition) { d.Light["background"] = "url(https://outside.example)" },
		func(d *model.ThemeDefinition) { delete(d.Dark, "foreground") }, func(d *model.ThemeDefinition) { d.Light["unknown"] = "#ffffff" },
		func(d *model.ThemeDefinition) { d.Resources = []string{"cpu", "cpu"} },
		func(d *model.ThemeDefinition) { d.Resources = []string{"cpu", "gpu"} },
		func(d *model.ThemeDefinition) { d.Resources = []string{"cpu", "memory", "disk", "cpu"} },
		func(d *model.ThemeDefinition) { d.Font = "comic-sans" }, func(d *model.ThemeDefinition) { d.Font = "url(https://outside.example/font.woff2)" },
	} {
		candidate := validDefinition()
		mutate(&candidate)
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseDefinition(raw); err == nil {
			t.Fatal("invalid definition accepted")
		}
	}
	if _, err := ParseDefinition([]byte(`{"schema_version":1,"script":"danger"}`)); err == nil {
		t.Fatal("unknown fields accepted")
	}
}

func TestDefinitionOverviewLayouts(t *testing.T) {
	// 作者可选的布局加上站点可覆盖的布局都必须逐值通过校验。
	for _, overview := range []string{"cards", "table", "tiles", "list"} {
		definition := validDefinition()
		definition.Overview = overview
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		if parsed, err := ParseDefinition(raw); err != nil || parsed.Overview != overview {
			t.Fatalf("overview %q rejected or lost: %+v %v", overview, parsed, err)
		}
		if _, err := ParseThemeConfiguration([]byte(`{"overview":"`+overview+`"}`), nil); err != nil {
			t.Fatalf("site override %q rejected: %v", overview, err)
		}
	}
	for _, overview := range []string{"list-view", "LIST", "grid"} {
		if _, err := ParseThemeConfiguration([]byte(`{"overview":"`+overview+`"}`), nil); err == nil {
			t.Fatalf("unknown site override %q accepted", overview)
		}
	}
}

func TestDefinitionAuthorContentFields(t *testing.T) {
	hidden := false
	definition := validDefinition()
	definition.Resources = []string{model.ThemeResourceMemory, model.ThemeResourceCPU}
	definition.ShowRemark = &hidden
	definition.ShowNetwork = &hidden
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDefinition(raw)
	if err != nil {
		t.Fatalf("author content fields rejected: %v", err)
	}
	if len(parsed.Resources) != 2 || parsed.Resources[0] != "memory" || parsed.ShowRemark == nil || *parsed.ShowRemark {
		t.Fatalf("author content fields not preserved: %+v", parsed)
	}
	// 省略这些字段的既有主题保持默认：全部资源、全部开关为 true。
	legacy := validDefinition()
	raw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = ParseDefinition(raw)
	if err != nil || len(parsed.Resources) != 0 || parsed.ShowNetwork != nil || parsed.ShowTags != nil {
		t.Fatalf("legacy definition changed: %+v %v", parsed, err)
	}
	// 站点覆盖只作用于四个显示字段，不吸收作者的内容开关。
	resolved := ApplyThemeConfiguration(parsed, model.ThemeConfiguration{})
	if resolved.ShowTags != nil || resolved.Resources != nil {
		t.Fatal("site configuration must not touch author content fields")
	}
}

func TestDefinitionAuthorFontField(t *testing.T) {
	for _, font := range model.ThemeFonts {
		definition := validDefinition()
		definition.Font = font
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseDefinition(raw)
		if err != nil || parsed.Font != font {
			t.Fatalf("font %s rejected or lost: %+v %v", font, parsed, err)
		}
	}
	// 省略字体的既有主题保持产品默认字体；没有声明 configuration 的主题也不接受站点改字体。
	legacy := validDefinition()
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDefinition(raw)
	if err != nil || parsed.Font != "" {
		t.Fatalf("legacy definition changed: %+v %v", parsed, err)
	}
	resolved := ApplyThemeConfiguration(parsed, model.ThemeConfiguration{})
	if resolved.Font != "" {
		t.Fatal("site configuration must not touch the author font")
	}
}

func TestThemePaletteContrastAndConfiguration(t *testing.T) {
	definition := validDefinition()
	definition.Light["foreground"] = "#eeeeee"
	if err := ValidateDefinition(definition); err == nil {
		t.Fatal("low-contrast text accepted")
	}
	definition = validDefinition()
	definition.Dark["chart-1"] = "#ffffff"
	if err := ValidateDefinition(definition); err == nil {
		t.Fatal("invisible chart line accepted")
	}
	configuration, err := ParseThemeConfiguration([]byte(`{"overview":"table","detail":"compact","density":"compact","radius":4}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved := ApplyThemeConfiguration(validDefinition(), configuration)
	if resolved.Overview != "table" || resolved.Detail != "compact" || resolved.Radius != 4 {
		t.Fatalf("configuration not applied: %+v", resolved)
	}
	for _, raw := range []string{`{"radius":99}`, `{"overview":"html"}`, `{"__proto__":{}}`, `{"css":"body{}"}`, `{} {}`} {
		if _, err := ParseThemeConfiguration([]byte(raw), nil); err == nil {
			t.Fatalf("invalid configuration accepted: %s", raw)
		}
	}
}
