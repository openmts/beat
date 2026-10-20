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
