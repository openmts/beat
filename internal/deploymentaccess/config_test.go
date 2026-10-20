package deploymentaccess

import (
	"errors"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseOriginsAcceptsExactOriginsAndNormalizes(t *testing.T) {
	cases := []struct {
		name   string
		value  string
		want   []string
		mode   string
		count  int
		allows []string
		denied []string
	}{
		{
			name: "explicit https origins", value: "https://console.example.com,https://status.example.com:8443",
			want: []string{"https://console.example.com", "https://status.example.com:8443"},
			mode: OriginModeConfigured, count: 2,
			allows: []string{"https://console.example.com", "https://CONSOLE.example.com/", "https://status.example.com:8443"},
			denied: []string{"https://evil.example.com", "https://console.example.com:8443", "", "not-an-origin", "https://console.example.com/path"},
		},
		{
			name: "default ports are dropped", value: "HTTPS://Console.Example.com:443/,http://console.example.com:80",
			want: []string{"http://console.example.com", "https://console.example.com"},
			mode: OriginModeConfigured, count: 2,
			allows: []string{"https://console.example.com", "http://console.example.com"},
		},
		{
			name: "ipv6 authority", value: "https://[2001:DB8::1]:443",
			want: []string{"https://[2001:db8::1]"}, mode: OriginModeConfigured, count: 1,
			allows: []string{"https://[2001:db8::1]:443"},
		},
		{
			name: "wildcard", value: "*", want: []string{}, mode: OriginModeAll, count: 0,
			allows: []string{"https://anything.example.com", "http://localhost:5173"},
		},
		{
			name: "empty means same origin", value: "  ", want: []string{}, mode: OriginModeSameOrigin, count: 0,
			denied: []string{"https://anywhere.example.com"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			policy, err := ParseOrigins(testCase.value)
			if err != nil {
				t.Fatalf("ParseOrigins(%q) returned %v", testCase.value, err)
			}
			if got := policy.Origins(); !equalStrings(got, testCase.want) {
				t.Fatalf("origins = %v, want %v", got, testCase.want)
			}
			if policy.Mode() != testCase.mode || policy.Count() != testCase.count {
				t.Fatalf("mode/count = %s/%d, want %s/%d", policy.Mode(), policy.Count(), testCase.mode, testCase.count)
			}
			for _, origin := range testCase.allows {
				if !policy.Allows(origin) {
					t.Errorf("expected %q to be allowed", origin)
				}
			}
			for _, origin := range testCase.denied {
				if policy.Allows(origin) {
					t.Errorf("expected %q to be denied", origin)
				}
			}
		})
	}
}

func TestParseOriginsRejectsUnsafeInput(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"wildcard mixed with exact", "https://console.example.com,*", ErrOriginWildcardMixed},
		{"wildcard mixed in list", "*,https://console.example.com", ErrOriginWildcardMixed},
		{"duplicate after normalization", "https://console.example.com,HTTPS://CONSOLE.example.com:443", ErrOriginDuplicate},
		{"userinfo", "https://user:secret@console.example.com", ErrOriginInvalid},
		{"opaque", "mailto:ops@example.com", ErrOriginInvalid},
		{"non http scheme", "ftp://console.example.com", ErrOriginInvalid},
		{"path", "https://console.example.com/admin", ErrOriginInvalid},
		{"query", "https://console.example.com/?token=1", ErrOriginInvalid},
		{"fragment", "https://console.example.com/#frag", ErrOriginInvalid},
		{"embedded nul", "https://console.example.com\x00", ErrOriginInvalid},
		{"embedded tab in host", "https://cons\trol.example.com", ErrOriginInvalid},
		{"missing host", "https:///admin", ErrOriginInvalid},
		{"empty entry", "https://console.example.com,,https://status.example.com", ErrOriginInvalid},
		{"oversized", "https://" + strings.Repeat("a", 300) + ".example.com", ErrOriginInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParseOrigins(testCase.value); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
	tooMany := strings.Repeat("https://a.example.com,", 64) + "https://b.example.com"
	if _, err := ParseOrigins(tooMany); !errors.Is(err, ErrOriginInvalid) {
		t.Fatalf("expected oversized list to be rejected, got %v", err)
	}
}

func TestParsePublicBaseURLRequiresTrustedOrigin(t *testing.T) {
	allowed := map[string]string{
		"":                              "",
		"   ":                           "",
		"https://beat.example.com":      "https://beat.example.com",
		"HTTPS://Beat.Example.com:443/": "https://beat.example.com",
		"https://[2001:db8::1]:8443":    "https://[2001:db8::1]:8443",
		"http://localhost:8080":         "http://localhost:8080",
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080",
		"http://[::1]:8080":             "http://[::1]:8080",
	}
	for input, want := range allowed {
		got, err := ParsePublicBaseURL(input)
		if err != nil || got != want {
			t.Fatalf("ParsePublicBaseURL(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	rejected := []string{
		"http://beat.example.com", "https://user@beat.example.com", "https://beat.example.com/base",
		"https://beat.example.com/?a=1", "https://beat.example.com/#frag", "beat.example.com",
		"javascript:alert(1)", "https://" + strings.Repeat("a", 300) + ".example.com",
	}
	for _, input := range rejected {
		if _, err := ParsePublicBaseURL(input); !errors.Is(err, ErrPublicBaseURLInvalid) {
			t.Errorf("expected %q to be rejected, got %v", input, err)
		}
	}
}

func TestLoadDefaultsPreserveCurrentPublicBehavior(t *testing.T) {
	config, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if config.PublicBaseURL() != "" {
		t.Fatalf("expected no canonical URL by default, got %q", config.PublicBaseURL())
	}
	if !config.APIOrigins().Wildcard() {
		t.Fatalf("public REST must keep allowing every origin by default")
	}
	if config.WSOrigins().Mode() != OriginModeSameOrigin {
		t.Fatalf("public WebSocket must default to same origin, got %s", config.WSOrigins().Mode())
	}
	status := config.Status()
	if !status.Valid || status.APIOriginMode != OriginModeAll || status.WSOriginMode != OriginModeSameOrigin {
		t.Fatalf("unexpected status %+v", status)
	}
	if status.PublicBaseURL != "" || status.APIOriginCount != 0 || status.WSOriginCount != 0 {
		t.Fatalf("status must not expose an origin list: %+v", status)
	}
}

func TestLoadHonoursConfiguredPolicy(t *testing.T) {
	config, err := Load(env(map[string]string{
		EnvPublicBaseURL:   "https://beat.example.com",
		EnvPublicAPIOrigin: "https://console.example.com",
		EnvPublicWSOrigin:  "https://console.example.com,https://status.example.com",
	}))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if config.PublicBaseURL() != "https://beat.example.com" {
		t.Fatalf("unexpected URL %q", config.PublicBaseURL())
	}
	if config.APIOrigins().Mode() != OriginModeConfigured || config.APIOrigins().Count() != 1 {
		t.Fatalf("unexpected API policy %+v", config.APIOrigins())
	}
	if !config.WSOrigins().Allows("https://status.example.com") {
		t.Fatalf("expected configured WebSocket origin to be allowed")
	}
	status := config.Status()
	if status.WSOriginCount != 2 || status.APIOriginCount != 1 || !strings.HasPrefix(status.PublicBaseURL, "https://") {
		t.Fatalf("unexpected status %+v", status)
	}
}

func TestLoadFallsBackToCanonicalURLForWebSockets(t *testing.T) {
	config, err := Load(env(map[string]string{EnvPublicBaseURL: "https://beat.example.com"}))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if config.WSOrigins().Mode() != OriginModeConfigured || !config.WSOrigins().Allows("https://beat.example.com") {
		t.Fatalf("expected WebSocket origins to follow the canonical URL, got %+v", config.WSOrigins())
	}
}

func TestLoadRejectsInvalidDeploymentValues(t *testing.T) {
	cases := []map[string]string{
		{EnvPublicBaseURL: "http://beat.example.com"},
		{EnvPublicAPIOrigin: "https://console.example.com,*"},
		{EnvPublicWSOrigin: "console.example.com"},
	}
	for _, values := range cases {
		if _, err := Load(env(values)); err == nil {
			t.Fatalf("expected %v to be rejected", values)
		}
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
