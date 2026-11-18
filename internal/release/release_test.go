package release

import (
	"strings"
	"testing"
)

func TestAssetURL(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{
			name: "latest",
			tag:  "latest",
			want: "https://github.com/openmts/beat/releases/latest/download/beat-agent-linux-amd64",
		},
		{
			name: "version tag",
			tag:  "v0.2.0",
			want: "https://github.com/openmts/beat/releases/download/v0.2.0/beat-agent-linux-amd64",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AssetURL("openmts/beat", test.tag, "beat-agent-linux-amd64"); got != test.want {
				t.Fatalf("AssetURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateAgentDownloadURL(t *testing.T) {
	repository := "openmts/beat"
	arch := "amd64"
	valid := AssetURL(repository, "v0.2.0", "beat-agent-linux-amd64")
	if err := ValidateAgentDownloadURL(valid, repository, arch); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
	latest := AssetURL(repository, "latest", "beat-agent-linux-amd64")
	if err := ValidateAgentDownloadURL(latest, repository, arch); err != nil {
		t.Fatalf("valid latest URL rejected: %v", err)
	}

	tests := []struct {
		name string
		url  string
		arch string
	}{
		{name: "http", url: "http://github.com/openmts/beat/releases/download/v0.2.0/beat-agent-linux-amd64", arch: arch},
		{name: "wrong host", url: "https://evil.example/openmts/beat/releases/download/v0.2.0/beat-agent-linux-amd64", arch: arch},
		{name: "wrong repository", url: "https://github.com/example/beat/releases/download/v0.2.0/beat-agent-linux-amd64", arch: arch},
		{name: "wrong asset", url: "https://github.com/openmts/beat/releases/download/v0.2.0/beat-server-linux-amd64", arch: arch},
		{name: "wrong architecture", url: "https://github.com/openmts/beat/releases/download/v0.2.0/beat-agent-linux-arm64", arch: arch},
		{name: "query string", url: valid + "?download=1", arch: arch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateAgentDownloadURL(test.url, repository, test.arch); err == nil {
				t.Fatalf("invalid URL accepted: %s", test.url)
			}
		})
	}
}

func TestManifestValidate(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest := Manifest{
		SchemaVersion: 1,
		Version:       "v0.2.0",
		ReleaseTag:    "v0.2.0",
		Repository:    "openmts/beat",
		Assets: Assets{Agent: AgentAssets{
			AMD64: Asset{Name: "beat-agent-linux-amd64", URL: AssetURL("openmts/beat", "v0.2.0", "beat-agent-linux-amd64"), Size: 10, SHA256: digest},
			ARM64: Asset{Name: "beat-agent-linux-arm64", URL: AssetURL("openmts/beat", "v0.2.0", "beat-agent-linux-arm64"), Size: 11, SHA256: digest},
		}},
	}
	if err := manifest.Validate("openmts/beat"); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	manifest.Assets.Agent.ARM64.URL = manifest.Assets.Agent.AMD64.URL
	if err := manifest.Validate("openmts/beat"); err == nil {
		t.Fatal("manifest with mismatched architecture URL was accepted")
	}
}

func TestValidateRedirectURL(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/openmts/beat/releases/download/v0.2.0/beat-agent-linux-amd64",
		"https://objects.githubusercontent.com/release-asset",
		"https://release-assets.githubusercontent.com/release-asset",
	} {
		if err := ValidateRedirectURL(raw); err != nil {
			t.Fatalf("valid redirect rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://github.com/openmts/beat",
		"https://github.com.evil.example/release",
		"https://evil.example/release",
	} {
		if err := ValidateRedirectURL(raw); err == nil {
			t.Fatalf("invalid redirect accepted: %s", raw)
		}
	}
}

func TestExpectedSHA256(t *testing.T) {
	if !ExpectedSHA256(strings.Repeat("a", 64)) {
		t.Fatal("valid SHA-256 rejected")
	}
	for _, value := range []string{"", "abc", strings.Repeat("z", 64)} {
		if ExpectedSHA256(value) {
			t.Fatalf("invalid SHA-256 accepted: %q", value)
		}
	}
}
