package loginsecurity

import (
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestSummaryFromSourcePrefersPrefixClassAndUnknown(t *testing.T) {
	tests := []struct {
		name   string
		class  string
		prefix string
		want   string
	}{
		{name: "prefix wins", class: model.SecuritySourceClassPublic, prefix: "203.0.113.0/24", want: "203.0.113.0/24"},
		{name: "class without prefix", class: model.SecuritySourceClassLoopback, want: model.SecuritySourceClassLoopback},
		{name: "unknown fallback", want: model.SecuritySourceClassUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SummaryFromSource(test.class, test.prefix); got != test.want {
				t.Fatalf("SummaryFromSource(%q, %q) = %q, want %q", test.class, test.prefix, got, test.want)
			}
		})
	}
}

func TestSummaryFromDeviceJoinsKnownPartsAndFallsBackToUnknown(t *testing.T) {
	tests := []struct {
		name    string
		browser string
		system  string
		class   string
		parts   []string
	}{
		{name: "all parts", browser: "Chrome", system: "Linux", class: model.SecurityDeviceClassDesktop,
			parts: []string{"Chrome", "Linux", model.SecurityDeviceClassDesktop}},
		{name: "unknown class skipped", browser: "Safari", system: "iOS", class: model.SecurityDeviceClassUnknown,
			parts: []string{"Safari", "iOS"}},
		{name: "class only", class: model.SecurityDeviceClassAutomation,
			parts: []string{model.SecurityDeviceClassAutomation}},
		{name: "empty parts", parts: nil},
		{name: "unknown only", class: model.SecurityDeviceClassUnknown, parts: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := model.SecurityDeviceClassUnknown
			if len(test.parts) > 0 {
				want = strings.Join(test.parts, " · ")
			}
			got := SummaryFromDevice(test.browser, test.system, test.class)
			if got != want {
				t.Fatalf("SummaryFromDevice(%q, %q, %q) = %q, want %q",
					test.browser, test.system, test.class, got, want)
			}
		})
	}
}
