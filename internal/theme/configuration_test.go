package theme

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func managedConfiguration(items ...map[string]any) map[string]any {
	declared := make([]any, 0, len(items))
	for _, item := range items {
		declared = append(declared, item)
	}
	return map[string]any{"type": "managed", "items": declared}
}

func TestManifestAcceptsManagedConfiguration(t *testing.T) {
	manifest, err := parseManifest([]byte(sampleManifest(t, map[string]any{"configuration": managedConfiguration(
		map[string]any{"key": "overview", "name": "Layout", "options": []string{"cards", "table"}},
		map[string]any{"key": "radius"},
		map[string]any{"key": "show_host"},
		map[string]any{"key": "font", "options": []string{"mono"}},
	)})))
	if err != nil {
		t.Fatalf("managed configuration rejected: %v", err)
	}
	described := describeConfigurationSchema(manifest.Configuration)
	if described == nil || described.Type != model.ThemeConfigurationManaged || len(described.Items) != 4 {
		t.Fatalf("described schema = %+v", described)
	}
	if described.Items[0].Type != model.ThemeConfigurationSelect || len(described.Items[0].Options) != 2 {
		t.Fatalf("narrowed options lost: %+v", described.Items[0])
	}
	if described.Items[1].Type != model.ThemeConfigurationNumber || described.Items[2].Type != model.ThemeConfigurationSwitch {
		t.Fatalf("derived types wrong: %+v", described.Items)
	}
	// 空 items 或没有声明 configuration 都退回历史四字段口径。
	if describeConfigurationSchema(&model.ThemeConfigurationSchema{Type: model.ThemeConfigurationManaged}) != nil {
		t.Fatal("empty item list described a schema")
	}
	if describeConfigurationSchema(nil) != nil {
		t.Fatal("missing configuration described a schema")
	}
}

func TestManifestRejectsInvalidConfiguration(t *testing.T) {
	tooMany := make([]any, 0, model.ThemeConfigurationItemLimit+1)
	for index := 0; index <= model.ThemeConfigurationItemLimit; index++ {
		tooMany = append(tooMany, map[string]any{"key": "show_host"})
	}
	cases := map[string]any{
		"raw type":             map[string]any{"type": "raw", "items": []any{}},
		"redirect type":        map[string]any{"type": "redirect", "items": []any{}},
		"missing type":         map[string]any{"items": []any{map[string]any{"key": "overview"}}},
		"too many items":       map[string]any{"type": "managed", "items": tooMany},
		"unknown key":          managedConfiguration(map[string]any{"key": "css"}),
		"array key":            managedConfiguration(map[string]any{"key": "resources"}),
		"duplicate key":        managedConfiguration(map[string]any{"key": "overview"}, map[string]any{"key": "overview"}),
		"author type":          managedConfiguration(map[string]any{"key": "overview", "type": "switch"}),
		"option outside legal": managedConfiguration(map[string]any{"key": "overview", "options": []string{"grid"}}),
		"duplicate options":    managedConfiguration(map[string]any{"key": "detail", "options": []string{"compact", "compact"}}),
		"options on number":    managedConfiguration(map[string]any{"key": "radius", "options": []string{"1"}}),
		"options on switch":    managedConfiguration(map[string]any{"key": "show_host", "options": []string{"on"}}),
		"oversized name":       managedConfiguration(map[string]any{"key": "overview", "name": strings.Repeat("a", maxConfigurationItemName+1)}),
		"malformed items":      map[string]any{"type": "managed", "items": "overview"},
	}
	for name, configuration := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseManifest([]byte(sampleManifest(t, map[string]any{"configuration": configuration}))); err == nil {
				t.Fatalf("accepted invalid configuration: %s", name)
			}
		})
	}
	sandboxed := sampleManifest(t, map[string]any{"runtime": model.ThemeRuntimeSandboxed, "configuration": managedConfiguration(map[string]any{"key": "overview"})})
	if _, err := parseManifest([]byte(sandboxed)); err == nil {
		t.Fatal("sandboxed runtime accepted a configuration schema")
	}
}

func configurationSchemaService(t *testing.T, configuration map[string]any) (*Service, *model.ThemePackage) {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	manifest := sampleManifest(t, map[string]any{"configuration": configuration})
	raw := buildThemeZip(t, manifest, map[string]string{"preview.png": fixturePNG, "assets/theme.css": "c"})
	pkg, err := service.Install(t.Context(), raw, model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	return service, pkg
}

func TestDescribeDeclaredConfigurationSchema(t *testing.T) {
	service, pkg := configurationSchemaService(t, managedConfiguration(
		map[string]any{"key": "overview", "options": []string{"cards", "table"}},
		map[string]any{"key": "font"},
		map[string]any{"key": "show_host"},
	))
	descriptor, err := service.DescribePackage(t.Context(), pkg.ID)
	if err != nil || descriptor.ConfigurationSchema == nil || len(descriptor.ConfigurationSchema.Items) != 3 {
		t.Fatalf("descriptor schema: %+v %v", descriptor.ConfigurationSchema, err)
	}
	if len(descriptor.ConfigurationSchema.Items[1].Options) != len(model.ThemeFonts) {
		t.Fatalf("font options not defaulted: %+v", descriptor.ConfigurationSchema.Items[1])
	}
	configured, err := service.Configure(t.Context(), pkg.ID, []byte(`{"overview":"table","font":"mono","show_host":false}`))
	if err != nil {
		t.Fatalf("declared configuration rejected: %v", err)
	}
	if configured.Declarative == nil || configured.Declarative.Overview != "table" || configured.Declarative.Font != "mono" {
		t.Fatalf("declared configuration not applied: %+v", configured.Declarative)
	}
	if configured.Declarative.ShowHost == nil || *configured.Declarative.ShowHost {
		t.Fatalf("declared switch not applied: %+v", configured.Declarative.ShowHost)
	}
	if configured.Configuration != `{"overview":"table","font":"mono","show_host":false}` {
		t.Fatalf("stored configuration = %s", configured.Configuration)
	}
	reset, err := service.Configure(t.Context(), pkg.ID, []byte(`{}`))
	if err != nil || reset.Declarative.ShowHost != nil || reset.Declarative.Font != "" {
		t.Fatalf("reset did not clear the site configuration: %+v %v", reset.Declarative, err)
	}
}

func TestConfigureRejectsUndeclaredOrInvalidValues(t *testing.T) {
	service, pkg := configurationSchemaService(t, managedConfiguration(
		map[string]any{"key": "overview", "options": []string{"cards", "table"}},
		map[string]any{"key": "radius"},
		map[string]any{"key": "show_host"},
	))
	for _, raw := range []string{
		`{"detail":"compact"}`, `{"font":"mono"}`, `{"show_tags":false}`, `{"overview":"list"}`,
		`{"radius":9}`, `{"radius":-1}`, `{"show_host":"yes"}`, `{"overview":{"key":"cards"}}`,
		`{"overview":"cards","radius":2.5}`, `{"resources":["cpu"]}`,
	} {
		if _, err := service.Configure(t.Context(), pkg.ID, []byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	legacy, legacyPkg := configurationSchemaService(t, nil)
	descriptor, err := legacy.DescribePackage(t.Context(), legacyPkg.ID)
	if err != nil || descriptor.ConfigurationSchema != nil {
		t.Fatalf("legacy package published a configuration schema: %+v %v", descriptor.ConfigurationSchema, err)
	}
	if _, err := legacy.Configure(t.Context(), legacyPkg.ID, []byte(`{"font":"mono"}`)); err == nil {
		t.Fatal("legacy package accepted a font override")
	}
	if _, err := legacy.Configure(t.Context(), legacyPkg.ID, []byte(`{"overview":"tiles","radius":4}`)); err != nil {
		t.Fatalf("legacy package rejected the historical fields: %v", err)
	}
}
