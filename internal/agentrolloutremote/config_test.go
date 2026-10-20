package agentrolloutremote

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
)

func TestConfigurationValidatesSourceAndBuildsFixedURLs(t *testing.T) {
	rollout := &agentrollout.Service{}
	for name, config := range map[string]Config{
		"missing rollout":      {Repository: DefaultRepository, APIURL: DefaultAPIURL},
		"repository traversal": {Rollout: rollout, Repository: "../beat", APIURL: DefaultAPIURL},
		"repository current":   {Rollout: rollout, Repository: "./beat", APIURL: DefaultAPIURL},
		"token injection":      {Rollout: rollout, Repository: DefaultRepository, APIURL: DefaultAPIURL, Token: "a\nb"},
		"insecure remote API":  {Rollout: rollout, Repository: DefaultRepository, APIURL: "http://github.example"},
		"API credentials":      {Rollout: rollout, Repository: DefaultRepository, APIURL: "https://user:secret@api.github.com"},
		"API query":            {Rollout: rollout, Repository: DefaultRepository, APIURL: "https://api.github.com?x=1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(config); err == nil {
				t.Fatal("unsafe source configuration accepted")
			}
		})
	}
	service, err := New(Config{Rollout: rollout, HTTPClient: &http.Client{}, Now: func() time.Time { return time.Unix(1, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if service.repository != DefaultRepository || service.api.String() != DefaultAPIURL || service.now == nil {
		t.Fatalf("defaults = %+v", service)
	}
	target, err := service.repositoryURL("releases", "tags", "v1.2.3")
	if err != nil || target.String() != "https://api.github.com/repos/openmts/beat/releases/tags/v1.2.3" {
		t.Fatalf("target = %v, %v", target, err)
	}
}

func TestRedirectPolicyRestrictsProtocolsAndHosts(t *testing.T) {
	service := &Service{api: &url.URL{Scheme: "https", Host: "api.github.com"}}
	for name, target := range map[string]string{
		"API":               "https://api.github.com/repos/openmts/beat",
		"GitHub":            "https://github.com/openmts/beat/releases",
		"release assets":    "https://release-assets.githubusercontent.com/object",
		"nested GitHub CDN": "https://objects.githubusercontent.com/object",
	} {
		t.Run(name, func(t *testing.T) {
			parsed, _ := url.Parse(target)
			if !service.safeDownloadURL(parsed) {
				t.Fatalf("trusted redirect rejected: %s", target)
			}
		})
	}
	for _, target := range []string{
		"http://api.github.com/private", "https://evil.example/private",
		"https://githubusercontent.com.evil.example/private", "https://user@github.com/private",
		"https://github.com/private#fragment",
	} {
		parsed, _ := url.Parse(target)
		if service.safeDownloadURL(parsed) {
			t.Fatalf("unsafe redirect accepted: %s", target)
		}
	}
	if !loopbackHost("localhost") || !loopbackHost("127.0.0.1") || !loopbackHost("::1") || loopbackHost("github.com") {
		t.Fatal("loopback detection is incorrect")
	}
	if retryAfter("99") != 99*time.Second || retryAfter("bad") != time.Minute || retryAfter("9999") != time.Hour {
		t.Fatal("Retry-After parsing is not bounded")
	}
}
