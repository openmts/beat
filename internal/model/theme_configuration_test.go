package model

import (
	"slices"
	"testing"
)

func TestThemeConfigurationKeyTypeFollowsServerDerivedTypes(t *testing.T) {
	cases := map[string]string{
		"overview":     ThemeConfigurationSelect,
		"detail":       ThemeConfigurationSelect,
		"density":      ThemeConfigurationSelect,
		"font":         ThemeConfigurationSelect,
		"radius":       ThemeConfigurationNumber,
		"show_network": ThemeConfigurationSwitch,
		"show_host":    ThemeConfigurationSwitch,
		"show_tags":    ThemeConfigurationSwitch,
		"show_remark":  ThemeConfigurationSwitch,
		"resources":    "",
		"":             "",
	}
	for key, want := range cases {
		if got := ThemeConfigurationKeyType(key); got != want {
			t.Fatalf("key %q type = %q, want %q", key, got, want)
		}
	}
}

func TestThemeConfigurationKeyValuesAndNarrowing(t *testing.T) {
	cases := map[string][]string{
		"overview": {"cards", "table", "tiles", "list"},
		"detail":   {"balanced", "compact", "spotlight"},
		"density":  {"comfortable", "compact"},
		"font":     ThemeFonts,
	}
	for key, want := range cases {
		if got := ThemeConfigurationKeyValues(key); !slices.Equal(got, want) {
			t.Fatalf("key %q values = %v, want %v", key, got, want)
		}
	}
	for _, key := range []string{"radius", "show_tags", "resources", ""} {
		if got := ThemeConfigurationKeyValues(key); got != nil {
			t.Fatalf("non-select key %q returned %v", key, got)
		}
	}
	if got := ThemeConfigurationKeyOptions("density", []string{"compact"}); !slices.Equal(got, []string{"compact"}) {
		t.Fatalf("narrowed options = %v", got)
	}
	if got := ThemeConfigurationKeyOptions("density", nil); !slices.Equal(got, []string{"comfortable", "compact"}) {
		t.Fatalf("full options = %v", got)
	}
	if got := ThemeConfigurationKeyOptions("radius", []string{"0"}); !slices.Equal(got, []string{"0"}) {
		t.Fatalf("narrowed non-select options = %v", got)
	}
}

func TestThemeConfigurationKeysAreDeclarable(t *testing.T) {
	for _, key := range ThemeConfigurationKeys {
		if !IsThemeConfigurationKey(key) {
			t.Fatalf("declared key %q is not accepted", key)
		}
		if ThemeConfigurationKeyType(key) == "" {
			t.Fatalf("declared key %q has no derived type", key)
		}
	}
	for _, key := range []string{"resources", "secret", "configuration", ""} {
		if IsThemeConfigurationKey(key) {
			t.Fatalf("key %q must not be declarable", key)
		}
	}
	if len(ThemeConfigurationKeys) > ThemeConfigurationItemLimit {
		t.Fatalf("declarable keys %d exceed the item limit %d", len(ThemeConfigurationKeys), ThemeConfigurationItemLimit)
	}
}
