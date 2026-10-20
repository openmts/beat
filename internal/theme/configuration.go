package theme

import (
	"slices"

	"github.com/beat/backend/internal/model"
)

// legacyConfigurationKeys 是主题没有声明 configuration 时站点仍可覆盖的字段（历史口径）。
var legacyConfigurationKeys = []string{"overview", "detail", "density", "radius"}

// maxConfigurationItemName 限制后台表单里的显示名长度，避免清单塞入超长文案。
const maxConfigurationItemName = 64

// configurationPolicy 给出「可配置键 → 允许取值」映射：select 键映射到收窄后的取值集合，
// number/switch 键映射到 nil。主题没有声明 configuration 时沿用历史四字段口径。
func configurationPolicy(schema *model.ThemeConfigurationSchema) map[string][]string {
	if schema == nil || len(schema.Items) == 0 {
		return legacyConfigurationPolicy()
	}
	policy := make(map[string][]string, len(schema.Items))
	for _, item := range schema.Items {
		policy[item.Key] = model.ThemeConfigurationKeyOptions(item.Key, item.Options)
	}
	return policy
}

func legacyConfigurationPolicy() map[string][]string {
	policy := make(map[string][]string, len(legacyConfigurationKeys))
	for _, key := range legacyConfigurationKeys {
		policy[key] = model.ThemeConfigurationKeyValues(key)
	}
	return policy
}

// configurationValues 把已配置（指针非 nil）的字段摊平成「键 → 值」，供按类型校验。
func configurationValues(configuration model.ThemeConfiguration) map[string]any {
	values := map[string]any{}
	if configuration.Overview != nil {
		values["overview"] = *configuration.Overview
	}
	if configuration.Detail != nil {
		values["detail"] = *configuration.Detail
	}
	if configuration.Density != nil {
		values["density"] = *configuration.Density
	}
	if configuration.Radius != nil {
		values["radius"] = *configuration.Radius
	}
	if configuration.Font != nil {
		values["font"] = *configuration.Font
	}
	if configuration.ShowNetwork != nil {
		values["show_network"] = *configuration.ShowNetwork
	}
	if configuration.ShowHost != nil {
		values["show_host"] = *configuration.ShowHost
	}
	if configuration.ShowTags != nil {
		values["show_tags"] = *configuration.ShowTags
	}
	if configuration.ShowRemark != nil {
		values["show_remark"] = *configuration.ShowRemark
	}
	return values
}

func validateConfigurationValues(values map[string]any, policy map[string][]string) error {
	for key, value := range values {
		options, declared := policy[key]
		if !declared || !configurationValueAllowed(key, value, options) {
			return ErrManifestInvalid
		}
	}
	return nil
}

func configurationValueAllowed(key string, value any, options []string) bool {
	switch model.ThemeConfigurationKeyType(key) {
	case model.ThemeConfigurationSelect:
		text, ok := value.(string)
		return ok && slices.Contains(options, text)
	case model.ThemeConfigurationNumber:
		number, ok := value.(int)
		return ok && number >= model.ThemeRadiusMinimum && number <= model.ThemeRadiusMaximum
	case model.ThemeConfigurationSwitch:
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

// describeConfigurationSchema 把清单里声明的配置架构转成下发前端的形态：补齐服务端推导的类型与
// 生效取值。返回 nil 表示前端继续使用历史四字段表单。
func describeConfigurationSchema(schema *model.ThemeConfigurationSchema) *model.ThemeConfigurationSchema {
	if schema == nil || len(schema.Items) == 0 {
		return nil
	}
	described := model.ThemeConfigurationSchema{
		Type:  model.ThemeConfigurationManaged,
		Items: make([]model.ThemeConfigurationItem, 0, len(schema.Items)),
	}
	for _, item := range schema.Items {
		described.Items = append(described.Items, model.ThemeConfigurationItem{
			Key: item.Key, Name: item.Name, Type: model.ThemeConfigurationKeyType(item.Key),
			Options: model.ThemeConfigurationKeyOptions(item.Key, item.Options),
		})
	}
	return &described
}

// validateManifestConfiguration 校验作者声明的配置架构：只接受 managed，键必须可配置且不重复，
// options 只能收窄该键的合法取值，作者不得自己写 type。
func validateManifestConfiguration(schema *model.ThemeConfigurationSchema) error {
	if schema == nil {
		return nil
	}
	if schema.Type != model.ThemeConfigurationManaged || len(schema.Items) > model.ThemeConfigurationItemLimit {
		return ErrManifestInvalid
	}
	seen := make(map[string]bool, len(schema.Items))
	for _, item := range schema.Items {
		if err := validateConfigurationItem(item, seen); err != nil {
			return err
		}
		seen[item.Key] = true
	}
	return nil
}

func validateConfigurationItem(item model.ThemeConfigurationItem, seen map[string]bool) error {
	if item.Type != "" || !model.IsThemeConfigurationKey(item.Key) || seen[item.Key] || len(item.Name) > maxConfigurationItemName {
		return ErrManifestInvalid
	}
	return validateConfigurationOptions(item)
}

func validateConfigurationOptions(item model.ThemeConfigurationItem) error {
	legal := model.ThemeConfigurationKeyValues(item.Key)
	if len(item.Options) == 0 {
		return nil
	}
	if len(legal) == 0 || len(item.Options) > len(legal) {
		return ErrManifestInvalid
	}
	unique := make(map[string]bool, len(item.Options))
	for _, option := range item.Options {
		if unique[option] || !slices.Contains(legal, option) {
			return ErrManifestInvalid
		}
		unique[option] = true
	}
	return nil
}
