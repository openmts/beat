package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

const (
	minimumPreviewWidth  = 320
	minimumPreviewHeight = 180
)

// deliveredThemes 是随前端发布的声明式主题，必须能由 themes/<name> 源码逐字节复现。
var deliveredThemes = []string{"slate", "operations", "canvas", "graphite", "aurora", "terminal", "paper", "midnight", "solar"}

// deliveredSandboxThemes 是随前端发布的脚本主题（iframe 沙箱），同样要求逐字节复现。
var deliveredSandboxThemes = []string{"pulse"}

// shippedThemeNames 汇总全部已发布主题，供预览图与分发资产核对共用。
func shippedThemeNames() []string {
	return append(append([]string{}, deliveredThemes...), deliveredSandboxThemes...)
}

// shippedCopies 是构建脚本复制到前端的说明、SDK 与许可文件。
var shippedCopies = []struct{ shipped, source string }{
	{"webui/public/themes/theme-author-guide.md", "docs/beat/theme-author-guide.md"},
	{"webui/public/themes/theme-sandbox-contract.md", "docs/beat/theme-sandbox-contract.md"},
	{"webui/public/themes/theme-market-publishing.md", "docs/beat/theme-market-publishing.md"},
	{"webui/public/themes/beat-theme-sdk.js", "themes/sdk/bridge.js"},
	{"webui/public/themes/LICENSE", "themes/LICENSE"},
}

func TestDeliveredThemesAreDistinctDeclarativePackages(t *testing.T) {
	digests := make(map[string]string, len(deliveredThemes))
	definitions := make(map[string]string, len(deliveredThemes))
	for _, name := range deliveredThemes {
		validated, packed := packDeliveredTheme(t, name)
		if validated.Manifest.Runtime != "declarative" || validated.Manifest.SchemaVersion != 1 {
			t.Fatalf("theme %s must be a declarative schema 1 package", name)
		}
		if _, err := theme.ParseDefinition(validated.Content["theme.json"]); err != nil {
			t.Fatalf("theme %s definition is rejected: %v", name, err)
		}
		digests[name] = digestOf(t, packed)
		definitions[name] = digestOf(t, validated.Content["theme.json"])
	}
	if len(distinctValues(digests)) != len(deliveredThemes) {
		t.Fatalf("delivered theme archives are not distinct: %v", digests)
	}
	if len(distinctValues(definitions)) != len(deliveredThemes) {
		t.Fatalf("delivered theme definitions are not distinct: %v", definitions)
	}
}

func TestDeliveredSandboxThemesAreValidIframePackages(t *testing.T) {
	for _, name := range deliveredSandboxThemes {
		validated, _ := packDeliveredTheme(t, name)
		if validated.Manifest.Runtime != model.ThemeRuntimeSandboxed || validated.Manifest.SchemaVersion != 1 {
			t.Fatalf("theme %s must be a sandboxed schema 1 package", name)
		}
		entry, html := sandboxEntryOf(t, validated)
		for _, referenced := range []string{"theme.css", "theme.js"} {
			if !bytes.Contains(html, []byte(referenced)) {
				t.Fatalf("theme %s entry %s does not reference %s", name, entry, referenced)
			}
		}
		sdk, err := os.ReadFile(repositoryPath("themes", "sdk", "bridge.js"))
		if err != nil {
			t.Fatalf("theme SDK source is unreadable: %v", err)
		}
		if !bytes.Equal(validated.Content[filepath.Join(filepath.Dir(entry), "beat-sdk.js")], sdk) {
			t.Fatalf("theme %s must ship the repository SDK copy beside its entry", name)
		}
	}
}

// sandboxEntryOf 返回沙箱包唯一的入口 HTML 及其源码。
func sandboxEntryOf(t *testing.T, validated *theme.ValidatedPackage) (string, []byte) {
	t.Helper()
	entry := ""
	for _, file := range validated.Files {
		if strings.EqualFold(filepath.Ext(file.Path), ".html") {
			if entry != "" {
				t.Fatalf("sandboxed theme declares more than one HTML file: %s", file.Path)
			}
			entry = file.Path
		}
	}
	if entry == "" {
		t.Fatal("sandboxed theme declares no HTML entry")
	}
	return entry, validated.Content[entry]
}

func TestDeliveredThemePreviewsAreUsableRasters(t *testing.T) {
	for _, name := range shippedThemeNames() {
		validated, _ := packDeliveredTheme(t, name)
		preview := validated.Content[validated.Manifest.Preview]
		if len(preview) == 0 {
			t.Fatalf("theme %s does not ship the declared preview %q", name, validated.Manifest.Preview)
		}
		config, err := png.DecodeConfig(bytes.NewReader(preview))
		if err != nil {
			t.Fatalf("theme %s preview is not a PNG: %v", name, err)
		}
		if config.Width < minimumPreviewWidth || config.Height < minimumPreviewHeight {
			t.Fatalf("theme %s preview is too small: %dx%d", name, config.Width, config.Height)
		}
	}
}

func TestShippedThemeAssetsMatchSources(t *testing.T) {
	compared := 0
	for _, name := range shippedThemeNames() {
		_, packed := packDeliveredTheme(t, name)
		compared += compareShippedFile(t, filepath.Join("webui", "public", "themes", name+".theme.zip"), packed)
		preview, err := os.ReadFile(repositoryPath("themes", name, "preview.png"))
		if err != nil {
			t.Fatalf("theme %s preview source is unreadable: %v", name, err)
		}
		compared += compareShippedFile(t, filepath.Join("webui", "public", "themes", name+".preview.png"), preview)
	}
	for _, copy := range shippedCopies {
		source, err := os.ReadFile(repositoryPath(copy.source))
		if err != nil {
			t.Fatalf("source %s is unreadable: %v", copy.source, err)
		}
		compared += compareShippedFile(t, copy.shipped, source)
	}
	if compared == 0 {
		t.Log("no prebuilt theme assets found; run scripts/build-themes.sh to build and verify the shipped copies")
	}
}

// compareShippedFile 在构建产物存在时要求它与源码逐字节一致，返回被比较的文件数。
func compareShippedFile(t *testing.T, shipped string, expected []byte) int {
	t.Helper()
	content, err := os.ReadFile(repositoryPath(shipped))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("shipped file %s is unreadable: %v", shipped, err)
	}
	if !bytes.Equal(content, expected) {
		t.Fatalf("shipped file %s does not match its source; rerun scripts/build-themes.sh", shipped)
	}
	return 1
}

func packDeliveredTheme(t *testing.T, name string) (*theme.ValidatedPackage, []byte) {
	t.Helper()
	options := theme.PackOptions{
		Source: repositoryPath("themes", name),
		Output: repositoryPath("webui", "public", "themes", name+".theme.zip"),
	}
	packed, err := theme.BuildArchive(options)
	if err != nil {
		t.Fatalf("theme %s cannot be packed: %v", name, err)
	}
	validated, err := theme.ValidateArchive(packed)
	if err != nil {
		t.Fatalf("theme %s cannot be validated: %v", name, err)
	}
	return validated, packed
}

func repositoryPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

func digestOf(t *testing.T, content []byte) string {
	t.Helper()
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func distinctValues(values map[string]string) []string {
	seen := make([]string, 0, len(values))
	for _, value := range values {
		if !containsValue(seen, value) {
			seen = append(seen, value)
		}
	}
	return seen
}

func containsValue(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
