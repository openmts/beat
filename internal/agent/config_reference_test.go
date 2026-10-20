package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// 后台接入页要给出每个可配置字段的说明。这里用反射读取配置结构体的 JSON 标签，
// 与页面文案比对，避免文档落后于结构体（例如新增可选配置段却没人更新页面）。
var (
	documentedFieldName   = regexp.MustCompile(`name:\s*"([A-Za-z0-9_.\[\]]+)"`)
	documentedTranslation = regexp.MustCompile(`"setup\.field_([a-z_]+)"\s*:`)
	optionalSectionNames  = []string{"updates", "accelerators", "operations", "diagnostics"}
)

func TestConfigFieldReferenceCoversEveryConfigField(t *testing.T) {
	fields := jsonFieldTypes(reflect.TypeOf(configFile{}))
	if len(fields) == 0 {
		t.Fatal("configFile exposes no JSON fields")
	}
	reference := readDocumentedFile(t, "webui", "src", "pages", "admin", "agent-setup-optional-fields.ts")
	translations := readDocumentedFile(t, "webui", "src", "context", "agent-setup-translations.ts")
	documented := matchAll(documentedFieldName, reference)
	translated := matchAll(documentedTranslation, translations)
	for _, section := range optionalSectionNames {
		if _, ok := fields[section]; !ok {
			t.Fatalf("configFile no longer carries the %q section", section)
		}
		checkNestedReference(t, fields[section], section, documented)
	}
	for name, kind := range fields {
		if kind.Kind() == reflect.Struct || kind.Kind() == reflect.Slice || kind.Kind() == reflect.Pointer {
			continue
		}
		if !translated[name] {
			t.Errorf("%s: missing setup.field_%s translation in the required-field table", name, name)
		}
	}
}

func checkNestedReference(t *testing.T, kind reflect.Type, prefix string, documented map[string]bool) {
	t.Helper()
	for name, field := range jsonFieldTypes(kind) {
		if !documented[name] && !documented[prefix+"."+name] {
			t.Errorf("%s.%s: missing from the agent config field reference", prefix, name)
		}
		checkNestedReference(t, field, prefix+"."+name, documented)
	}
}

func jsonFieldTypes(kind reflect.Type) map[string]reflect.Type {
	for kind.Kind() == reflect.Pointer || kind.Kind() == reflect.Slice {
		kind = kind.Elem()
	}
	out := map[string]reflect.Type{}
	if kind.Kind() != reflect.Struct {
		return out
	}
	for index := 0; index < kind.NumField(); index++ {
		field := kind.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out[name] = field.Type
	}
	return out
}

func matchAll(pattern *regexp.Regexp, content string) map[string]bool {
	out := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(content, -1) {
		out[match[1]] = true
	}
	return out
}

func readDocumentedFile(t *testing.T, segments ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, segments...)...)
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
