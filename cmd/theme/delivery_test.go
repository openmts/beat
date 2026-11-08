package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

const (
	deliveredThemeName   = "default"
	minimumPreviewWidth  = 320
	minimumPreviewHeight = 180
)

var deliveredThemeNames = []string{"default", "paper"}

// shippedCopies 是构建脚本复制到前端的说明、SDK 与许可文件。
var shippedCopies = []struct{ shipped, source string }{
	{"webui/public/themes/theme-author-guide.md", "docs/beat/theme-author-guide.md"},
	{"webui/public/themes/theme-sandbox-contract.md", "docs/beat/theme-sandbox-contract.md"},
	{"webui/public/themes/theme-market-publishing.md", "docs/beat/theme-market-publishing.md"},
	{"webui/public/themes/beat-theme-sdk.js", "themes/sdk/bridge.js"},
	{"webui/public/themes/LICENSE", "themes/LICENSE"},
}

func TestDefaultThemeIsAValidSandboxPackage(t *testing.T) {
	validated, _ := packDeliveredTheme(t)
	if validated.Manifest.SchemaVersion != 1 || validated.Manifest.Runtime != model.ThemeRuntimeSandboxed {
		t.Fatalf("default theme must be a sandboxed schema 1 package")
	}
	entry, html := sandboxEntryOf(t, validated)
	for _, referenced := range []string{"theme.css", "components.css", "theme.js"} {
		if !bytes.Contains(html, []byte(referenced)) {
			t.Fatalf("default theme entry %s does not reference %s", entry, referenced)
		}
	}
	sdk, err := os.ReadFile(repositoryPath("themes", "sdk", "bridge.js"))
	if err != nil {
		t.Fatalf("theme SDK source is unreadable: %v", err)
	}
	if !bytes.Equal(validated.Content[filepath.Join(filepath.Dir(entry), "beat-sdk.js")], sdk) {
		t.Fatal("default theme must ship a byte-identical SDK beside its entry")
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

func TestDefaultThemePreviewIsUsable(t *testing.T) {
	validated, _ := packDeliveredTheme(t)
	assertThemePreview(t, validated)
}

func TestPaperThemeIsAValidSandboxPackage(t *testing.T) {
	validated, _ := packTheme(t, "paper")
	if validated.Manifest.SchemaVersion != 1 || validated.Manifest.Runtime != model.ThemeRuntimeSandboxed {
		t.Fatalf("paper theme must be a sandboxed schema 1 package")
	}
	entry, html := sandboxEntryOf(t, validated)
	for _, referenced := range []string{"theme.css", "theme.js"} {
		if !bytes.Contains(html, []byte(referenced)) {
			t.Fatalf("paper theme entry %s does not reference %s", entry, referenced)
		}
	}
	sdk, err := os.ReadFile(repositoryPath("themes", "sdk", "bridge.js"))
	if err != nil {
		t.Fatalf("theme SDK source is unreadable: %v", err)
	}
	if !bytes.Equal(validated.Content[filepath.Join(filepath.Dir(entry), "beat-sdk.js")], sdk) {
		t.Fatal("paper theme must ship a byte-identical SDK beside its entry")
	}
}

func TestPaperThemePreviewIsUsable(t *testing.T) {
	validated, _ := packTheme(t, "paper")
	assertThemePreview(t, validated)
}

func assertThemePreview(t *testing.T, validated *theme.ValidatedPackage) {
	t.Helper()
	preview := validated.Content[validated.Manifest.Preview]
	config, err := png.DecodeConfig(bytes.NewReader(preview))
	if err != nil {
		t.Fatalf("theme preview is not a PNG: %v", err)
	}
	if config.Width < minimumPreviewWidth || config.Height < minimumPreviewHeight {
		t.Fatalf("theme preview is too small: %dx%d", config.Width, config.Height)
	}
}

func TestThemeModuleGraphsAreSelfContained(t *testing.T) {
	for _, name := range deliveredThemeNames {
		t.Run(name, func(t *testing.T) {
			validated, _ := packTheme(t, name)
			relativeImport := regexp.MustCompile(`from\s+["'](\.[^"']+)["']`)
			for path, content := range validated.Content {
				if strings.ToLower(filepath.Ext(path)) != ".js" {
					continue
				}
				for _, match := range relativeImport.FindAllStringSubmatch(string(content), -1) {
					target := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(path), match[1])))
					if _, ok := validated.Content[target]; !ok {
						t.Errorf("%s imports missing module %s", path, target)
					}
				}
			}
		})
	}
}

func TestShippedThemeAssetsMatchSources(t *testing.T) {
	compared := 0
	for _, name := range deliveredThemeNames {
		_, packed := packTheme(t, name)
		compared += compareShippedFile(t, filepath.Join("webui", "public", "themes", name+".theme.zip"), packed)
		preview, err := os.ReadFile(repositoryPath("themes", name, "preview.png"))
		if err != nil {
			t.Fatalf("%s theme preview source is unreadable: %v", name, err)
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
		t.Log("no generated theme assets found; run scripts/build-themes.sh to verify the shipped copies")
	}
}

func TestShippedThemeDistributionHasNoUnexpectedPackages(t *testing.T) {
	paths, err := filepath.Glob(repositoryPath("webui", "public", "themes", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".theme.zip") && !strings.HasSuffix(name, ".preview.png") {
			continue
		}
		allowed := false
		for _, themeName := range deliveredThemeNames {
			if name == themeName+".theme.zip" || name == themeName+".preview.png" {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Errorf("unexpected bundled theme artifact: %s", path)
		}
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

func packDeliveredTheme(t *testing.T) (*theme.ValidatedPackage, []byte) {
	t.Helper()
	return packTheme(t, deliveredThemeName)
}

func packTheme(t *testing.T, name string) (*theme.ValidatedPackage, []byte) {
	t.Helper()
	packed, err := theme.BuildArchive(theme.PackOptions{
		Source: repositoryPath("themes", name),
		Output: filepath.Join(t.TempDir(), name+".theme.zip"),
	})
	if err != nil {
		t.Fatalf("%s theme cannot be packed: %v", name, err)
	}
	validated, err := theme.ValidateArchive(packed)
	if err != nil {
		t.Fatalf("%s theme cannot be validated: %v", name, err)
	}
	return validated, packed
}

func repositoryPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}
