package beatcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveServerAcceptsSupportedAddresses(t *testing.T) {
	cases := []struct{ raw, want string }{
		{raw: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{raw: "https://[::1]:8080/", want: "https://[::1]:8080"},
		{raw: "https://beat.example/beat/", want: "https://beat.example/beat"},
		{raw: "  http://192.168.1.48:8081  ", want: "http://192.168.1.48:8081"},
	}
	for _, scenario := range cases {
		parsed, err := ResolveServer(scenario.raw)
		if err != nil {
			t.Fatalf("expected %q to resolve: %v", scenario.raw, err)
		}
		if parsed.String() != scenario.want {
			t.Fatalf("expected %q, got %q", scenario.want, parsed.String())
		}
	}
}

func TestResolveServerRejectsUnsafeAddresses(t *testing.T) {
	cases := []string{
		"",
		"127.0.0.1:8080",
		"ftp://beat.example",
		"beat.example",
		"http://user:secret@beat.example",
		"http://beat.example/?token=1",
		"http://beat.example/#fragment",
		"http://:8080",
	}
	for _, raw := range cases {
		if _, err := ResolveServer(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestResolveTokenReadsSecureFile(t *testing.T) {
	path := writeTokenFile(t, 0o600, "beat_service_v1_abcdef\n")
	token, err := ResolveToken(path, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "beat_service_v1_abcdef" {
		t.Fatalf("expected trimmed token, got %q", token)
	}
}

func TestResolveTokenRejectsLoosePermissions(t *testing.T) {
	path := writeTokenFile(t, 0o644, "beat_service_v1_abcdef\n")
	if _, err := ResolveToken(path, nil, nil); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("expected a permission error, got %v", err)
	}
}

func TestResolveTokenRejectsInvalidFiles(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "empty", content: "   \n"},
		{name: "missing prefix", content: "n_abcdef"},
		{name: "inner whitespace", content: "beat_service_v1_ab cd"},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			path := writeTokenFile(t, 0o600, scenario.content)
			if _, err := ResolveToken(path, nil, nil); err == nil {
				t.Fatalf("expected %q to be rejected", scenario.content)
			}
		})
	}
	if _, err := ResolveToken(filepath.Join(t.TempDir(), "absent"), nil, nil); err == nil {
		t.Fatal("expected a missing file error")
	}
	if _, err := ResolveToken(t.TempDir(), nil, nil); err == nil {
		t.Fatal("expected a non-regular file error")
	}
}

func TestResolveTokenUsesEnvironmentFallback(t *testing.T) {
	env := func(name string) string {
		if name == ServiceTokenEnv {
			return " beat_service_v1_env \n"
		}
		return ""
	}
	token, err := ResolveToken("", env, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "beat_service_v1_env" {
		t.Fatalf("expected env token, got %q", token)
	}
	blank := func(string) string { return "   " }
	if _, err := ResolveToken("", blank, nil); err == nil || !strings.Contains(err.Error(), ServiceTokenEnv) {
		t.Fatalf("expected a missing token error, got %v", err)
	}
	if _, err := ResolveToken("", nil, nil); err == nil || !strings.Contains(err.Error(), "--token-file") {
		t.Fatalf("expected actionable guidance, got %v", err)
	}
}

func TestResolveTokenReadsStandardInput(t *testing.T) {
	token, err := ResolveToken("-", nil, strings.NewReader("beat_service_v1_stdin"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "beat_service_v1_stdin" {
		t.Fatalf("expected stdin token, got %q", token)
	}
	if _, err := ResolveToken("-", nil, nil); err == nil {
		t.Fatal("expected an unavailable stdin error")
	}
	if _, err := ResolveToken("-", nil, strings.NewReader("   ")); err == nil {
		t.Fatal("expected an empty token error")
	}
}

func writeTokenFile(t *testing.T, mode os.FileMode, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	// 显式 chmod，避免进程 umask 把宽松权限的用例静默收紧。
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod token file: %v", err)
	}
	return path
}
