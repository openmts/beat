package theme

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"

	"github.com/beat/backend/internal/model"
)

const definitionName = "theme.json"

const maxDefinitionBytes = 64 << 10

func ParseDefinition(raw []byte) (model.ThemeDefinition, error) {
	var definition model.ThemeDefinition
	if err := decodeThemeJSON(raw, &definition, maxDefinitionBytes); err != nil {
		return definition, err
	}
	return definition, ValidateDefinition(definition)
}

func ValidateDefinition(definition model.ThemeDefinition) error {
	if definition.SchemaVersion != 1 {
		return ErrManifestInvalid
	}
	if !validThemeLayout(definition.Overview, definition.Detail) {
		return ErrManifestInvalid
	}
	if !validAppearance(definition.Density, definition.Radius) {
		return ErrManifestInvalid
	}
	if !validThemeResources(definition.Resources) {
		return ErrManifestInvalid
	}
	if !validThemeFont(definition.Font) {
		return ErrManifestInvalid
	}
	if !validOptionalToken(definition.Surface, model.ThemeSurfaces) || !validSummary(definition.Summary) || !validShell(definition.Shell) {
		return ErrManifestInvalid
	}
	if err := validatePalette(definition.Light); err != nil {
		return err
	}
	return validatePalette(definition.Dark)
}

func validThemeLayout(overview, detail string) bool {
	validOverview := overview == "cards" || overview == "table" || overview == "tiles" || overview == "list"
	validDetail := detail == "balanced" || detail == "compact" || detail == "spotlight"
	return validOverview && validDetail
}

// validThemeResources 允许作者留空（等于全部资源），否则要求 1 到 3 个互不重复的已知资源。
func validThemeResources(resources []string) bool {
	if len(resources) == 0 {
		return true
	}
	seen := make(map[string]bool, len(resources))
	for _, resource := range resources {
		if !slices.Contains(model.ThemeResources, resource) || seen[resource] {
			return false
		}
		seen[resource] = true
	}
	return len(resources) <= len(model.ThemeResources)
}

// validThemeFont 允许作者留空（等于产品默认字体），否则只接受内置固定字体栈。
func validThemeFont(font string) bool {
	return font == "" || slices.Contains(model.ThemeFonts, font)
}

func validOptionalToken(value string, allowed []string) bool {
	return value == "" || slices.Contains(allowed, value)
}

func validSummary(summary *model.ThemeSummary) bool {
	if summary == nil {
		return true
	}
	if !validOptionalToken(summary.Layout, model.ThemeSummaryLayouts) || len(summary.Metrics) > len(model.ThemeSummaryMetrics) {
		return false
	}
	seen := make(map[string]bool, len(summary.Metrics))
	for _, metric := range summary.Metrics {
		if seen[metric] || !slices.Contains(model.ThemeSummaryMetrics, metric) {
			return false
		}
		seen[metric] = true
	}
	return true
}

func validShell(shell *model.ThemeShell) bool {
	return shell == nil || (validOptionalToken(shell.Background, model.ThemeShellBackgrounds) &&
		validOptionalToken(shell.Header, model.ThemeShellHeaders) &&
		validOptionalToken(shell.Footer, model.ThemeShellFooters))
}

func validAppearance(density string, radius int) bool {
	return (density == "comfortable" || density == "compact") &&
		radius >= model.ThemeRadiusMinimum && radius <= model.ThemeRadiusMaximum
}

func decodeThemeJSON(raw []byte, value any, maximum int) error {
	if len(raw) == 0 || len(raw) > maximum || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return ErrManifestInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrManifestInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrManifestInvalid
	}
	return nil
}

// ParseThemeConfiguration 解析站点配置：只能写主题在清单里声明过的键，取值按该键的类型与取值集合
// 校验；schema 为 nil 时沿用历史四字段口径。键与取值都是受信任渲染器内置的枚举，配置改不了样式代码。
func ParseThemeConfiguration(raw []byte, schema *model.ThemeConfigurationSchema) (model.ThemeConfiguration, error) {
	var configuration model.ThemeConfiguration
	if err := decodeThemeJSON(raw, &configuration, 16<<10); err != nil {
		return configuration, err
	}
	if err := validateConfigurationValues(configurationValues(configuration), configurationPolicy(schema)); err != nil {
		return configuration, err
	}
	return configuration, nil
}

func ApplyThemeConfiguration(definition model.ThemeDefinition, configuration model.ThemeConfiguration) model.ThemeDefinition {
	if configuration.Overview != nil {
		definition.Overview = *configuration.Overview
	}
	if configuration.Detail != nil {
		definition.Detail = *configuration.Detail
	}
	if configuration.Density != nil {
		definition.Density = *configuration.Density
	}
	if configuration.Radius != nil {
		definition.Radius = *configuration.Radius
	}
	if configuration.Font != nil {
		definition.Font = *configuration.Font
	}
	if configuration.Surface != nil {
		definition.Surface = *configuration.Surface
	}
	if configuration.SummaryLayout != nil {
		if definition.Summary == nil {
			definition.Summary = &model.ThemeSummary{}
		}
		definition.Summary.Layout = *configuration.SummaryLayout
	}
	if configuration.ShellBackground != nil || configuration.ShellHeader != nil || configuration.ShellFooter != nil {
		if definition.Shell == nil {
			definition.Shell = &model.ThemeShell{}
		}
		if configuration.ShellBackground != nil {
			definition.Shell.Background = *configuration.ShellBackground
		}
		if configuration.ShellHeader != nil {
			definition.Shell.Header = *configuration.ShellHeader
		}
		if configuration.ShellFooter != nil {
			definition.Shell.Footer = *configuration.ShellFooter
		}
	}
	if configuration.ShowNetwork != nil {
		definition.ShowNetwork = configuration.ShowNetwork
	}
	if configuration.ShowHost != nil {
		definition.ShowHost = configuration.ShowHost
	}
	if configuration.ShowTags != nil {
		definition.ShowTags = configuration.ShowTags
	}
	if configuration.ShowRemark != nil {
		definition.ShowRemark = configuration.ShowRemark
	}
	return definition
}
