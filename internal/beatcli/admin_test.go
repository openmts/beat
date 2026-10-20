package beatcli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleCookie = "beat_admin_session=session-value"

func cookieResponse(status int, body, cookie string) *http.Response {
	response := jsonResponse(status, body)
	if cookie != "" {
		response.Header.Set("Set-Cookie", cookie+"; Path=/; HttpOnly; SameSite=Strict")
	}
	return response
}

func runCommandWithStdin(
	t *testing.T, transport http.RoundTripper, getenv func(string) string, stdin string, args ...string,
) runResult {
	t.Helper()
	var stdout, stderr strings.Builder
	options := Options{Output: &stdout, Errors: &stderr, Stdin: strings.NewReader(stdin), Getenv: getenv}
	if transport != nil {
		options.Transport = transport
	}
	err := NewCommand(options).Run(t.Context(), append([]string{"beat-cli"}, args...))
	return runResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// adminTransport 按路径给出管理面会话所需的响应，并记录每次请求。
func adminTransport(t *testing.T, handler func(*http.Request) (*http.Response, error)) *stubTransport {
	t.Helper()
	return &stubTransport{handler: handler}
}

func erasureArgs(password string, extra ...string) []string {
	args := append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, password)
	return append(args, extra...)
}

func TestErasureListLogsInReauthenticatesAndReadsWithTheSessionCookie(t *testing.T) {
	paths := []string{}
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		switch request.URL.Path {
		case loginPath:
			if got := request.Header.Get("Origin"); got != "http://127.0.0.1:8080" {
				t.Errorf("login must carry the server origin, got %q", got)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read login body: %v", err)
			}
			var login struct{ Username, Password, TOTPCode string }
			if err := json.Unmarshal(body, &login); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if login.Username != "owner" || login.Password != "secret" {
				t.Errorf("unexpected login body %q", body)
			}
			return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
		case reauthenticatePath, sessionPath:
			if got := request.Header.Get("Cookie"); got != sampleCookie {
				t.Errorf("session requests must carry the cookie, got %q", got)
			}
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`), nil
		default:
			if got := request.Header.Get("Cookie"); got != sampleCookie {
				t.Errorf("admin read must carry the cookie, got %q", got)
			}
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"jobs":[]}}`), nil
		}
	})
	password := writeTokenFile(t, 0o600, "secret")
	result := runCommand(t, transport, nil, append(erasureArgs(password), "erasure", "list", "--limit", "5")...)
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	want := []string{"POST " + loginPath, "POST " + reauthenticatePath, "GET " + erasurePath}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected call order %v", paths)
	}
	if got := transport.requests[2].URL.RawQuery; got != "limit=5" {
		t.Fatalf("unexpected query %q", got)
	}
	if !strings.Contains(result.stdout, `"jobs": []`) {
		t.Fatalf("expected unwrapped jobs, got %q", result.stdout)
	}
}

func TestErasureCommandsRejectBadInputAndSurfaceLoginFailures(t *testing.T) {
	password := writeTokenFile(t, 0o600, "secret")
	failing := adminTransport(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnauthorized, `{"code":401,"message":"invalid credentials"}`), nil
	})
	result := runCommand(t, failing, nil, append(erasureArgs(password), "erasure", "list")...)
	if result.err == nil || !strings.Contains(result.err.Error(), "invalid credentials") {
		t.Fatalf("expected the login failure to surface, got %v", result.err)
	}
	for _, args := range [][]string{
		append(erasureArgs(password), "erasure", "get"),
		append(erasureArgs(password), "erasure", "cancel", "one", "two"),
		append(erasureArgs(password), "erasure", "list", "extra"),
		append(erasureArgs(password), "erasure", "list", "--"+limitFlag, "-1"),
	} {
		if result := runCommand(t, failing, nil, args...); result.err == nil {
			t.Fatalf("expected a local rejection for %v", args)
		}
	}
	empty := adminTransport(t, func(*http.Request) (*http.Response, error) {
		return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, ""), nil
	})
	result = runCommand(t, empty, nil, append(erasureArgs(password), "erasure", "list")...)
	if result.err == nil || !strings.Contains(result.err.Error(), "session cookie") {
		t.Fatalf("expected a missing-cookie error, got %v", result.err)
	}
}

func TestErasureCreatePostsTheBoundedRequestShape(t *testing.T) {
	var payload map[string]string
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != loginPath && request.URL.Path != reauthenticatePath {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read create body: %v", err)
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			return jsonResponse(http.StatusAccepted, `{"code":202,"message":"accepted","data":{"job":{"id":"j1"}}}`), nil
		}
		return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
	})
	args := append(erasureArgs(writeTokenFile(t, 0o600, "secret")), "erasure", "create",
		"--"+scopeFlag, "node", "--"+targetFlag, "n1", "--"+requestIDFlag, "8f14e45f-ceea-467a-9e4f-1f7d0a1b2c3d",
		"--"+confirmFlag, "delete node n1")
	result := runCommand(t, transport, nil, args...)
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	want := map[string]string{"scope": "node", "target_id": "n1",
		"request_id": "8f14e45f-ceea-467a-9e4f-1f7d0a1b2c3d", "confirmation": "delete node n1"}
	if len(payload) != len(want) {
		t.Fatalf("unexpected create payload %v", payload)
	}
	for key, value := range want {
		if payload[key] != value {
			t.Fatalf("expected %s=%q, got %q", key, value, payload[key])
		}
	}
}

func TestAdminCredentialsComeFromEnvironmentPrivateFileOrStdin(t *testing.T) {
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == loginPath {
			return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
		}
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"jobs":[]}}`), nil
	})
	env := func(name string) string {
		switch name {
		case AdminUserEnv:
			return "env-owner"
		case AdminPasswordEnv:
			return "env-password"
		case AdminTOTPEnv:
			return "123456"
		}
		return ""
	}
	result := runCommand(t, transport, env, append(serverArgs(), "erasure", "list")...)
	if result.err != nil {
		t.Fatalf("unexpected error with environment credentials: %v", result.err)
	}
	loose := writeTokenFile(t, 0o644, "secret")
	result = runCommand(t, transport, nil, append(erasureArgs(loose), "erasure", "list")...)
	if result.err == nil || !strings.Contains(result.err.Error(), "chmod 600") {
		t.Fatalf("expected a permission error, got %v", result.err)
	}
	result = runCommandWithStdin(t, transport, nil, "stdin-secret\n",
		append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, "-", "erasure", "list")...)
	if result.err != nil {
		t.Fatalf("unexpected error with stdin password: %v", result.err)
	}
	for _, args := range [][]string{
		append(serverArgs(), "--"+adminPasswordFlag, writeTokenFile(t, 0o600, "secret"), "erasure", "list"),
		append(serverArgs(), "--"+adminUserFlag, "owner", "erasure", "list"),
		append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, "-", "erasure", "list"),
	} {
		result := runCommand(t, transport, func(string) string { return "" }, args...)
		if result.err == nil {
			t.Fatalf("expected a missing credential error for %v", args)
		}
	}
}

func TestSessionFileIsReusedAndKeptPrivate(t *testing.T) {
	logins := 0
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case loginPath:
			logins++
			return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
		case sessionPath, reauthenticatePath:
			if request.Header.Get("Cookie") != sampleCookie {
				t.Errorf("unexpected cookie %q", request.Header.Get("Cookie"))
			}
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`), nil
		default:
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"jobs":[]}}`), nil
		}
	})
	password := writeTokenFile(t, 0o600, "secret")
	session := filepath.Join(t.TempDir(), "session")
	args := append(erasureArgs(password, "--"+sessionFileFlag, session), "erasure", "list")
	if result := runCommand(t, transport, nil, args...); result.err != nil {
		t.Fatalf("first run failed: %v", result.err)
	}
	info, err := os.Stat(session)
	if err != nil {
		t.Fatalf("stat session file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected a 0600 session file, got %v", info.Mode().Perm())
	}
	content, err := os.ReadFile(session)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if strings.TrimSpace(string(content)) != sampleCookie {
		t.Fatalf("unexpected session file content %q", content)
	}
	if result := runCommand(t, transport, nil, args...); result.err != nil {
		t.Fatalf("second run failed: %v", result.err)
	}
	if logins != 1 {
		t.Fatalf("expected the cached session to be reused, got %d logins", logins)
	}
	loose := writeTokenFile(t, 0o644, sampleCookie)
	if result := runCommand(t, transport, nil,
		append(erasureArgs(password, "--"+sessionFileFlag, loose), "erasure", "list")...); result.err == nil ||
		!strings.Contains(result.err.Error(), "chmod 600") {
		t.Fatalf("expected a session file permission error, got %v", result.err)
	}
	if err := os.Remove(session); err != nil {
		t.Fatalf("remove session file: %v", err)
	}
	if result := runCommand(t, transport, nil, append(erasureArgs(password, "--"+sessionFileFlag, session), "erasure", "list")...); result.err != nil {
		t.Fatalf("expected a fresh login after deletion, got %v", result.err)
	}
}

func TestOperationsTasksUseTheAdministratorSession(t *testing.T) {
	var cancelPath, cancelMethod string
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == loginPath:
			return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
		case request.URL.Path == reauthenticatePath:
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`), nil
		case strings.HasSuffix(request.URL.Path, "/cancel"):
			cancelPath, cancelMethod = request.URL.Path, request.Method
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"acknowledged":true}}`), nil
		default:
			if got := request.URL.Query(); got.Get("limit") != "2" || got.Get("cursor") != "next" {
				t.Errorf("unexpected task query %q", got)
			}
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"items":[]}}`), nil
		}
	})
	password := writeTokenFile(t, 0o600, "secret")
	if result := runCommand(t, transport, nil,
		append(erasureArgs(password), "operations", "tasks", "list", "--"+limitFlag, "2", "--cursor", "next")...); result.err != nil {
		t.Fatalf("unexpected list error: %v", result.err)
	}
	if result := runCommand(t, transport, nil,
		append(erasureArgs(password), "operations", "tasks", "cancel", "task/one")...); result.err != nil {
		t.Fatalf("unexpected cancel error: %v", result.err)
	}
	if cancelMethod != http.MethodPost || cancelPath != operationsTasksPath+"/task%2Fone/cancel" {
		t.Fatalf("unexpected cancel request %s %s", cancelMethod, cancelPath)
	}
}

func TestErasureSingleJobCommandsAndSecretLimits(t *testing.T) {
	paths := []string{}
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		if request.URL.Path == loginPath {
			return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
		}
		if request.URL.Path == reauthenticatePath {
			return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`), nil
		}
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"id":"job/one"}}`), nil
	})
	password := writeTokenFile(t, 0o600, "secret")
	for _, action := range []string{"get", "retry", "cancel"} {
		if result := runCommand(t, transport, nil, append(erasureArgs(password), "erasure", action, "job/one")...); result.err != nil {
			t.Fatalf("erasure %s failed: %v", action, result.err)
		}
	}
	want := erasurePath + "/job%2Fone"
	if paths[2] != "GET "+want || paths[5] != "POST "+want+"/retry" || paths[8] != "POST "+want+"/cancel" {
		t.Fatalf("unexpected erasure paths %v", paths)
	}

	directory := t.TempDir()
	for _, args := range [][]string{
		append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, directory, "erasure", "list"),
		append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, writeTokenFile(t, 0o600, strings.Repeat("p", maximumAdminSecretBytes+1)), "erasure", "list"),
		append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, writeTokenFile(t, 0o600, "secret"),
			"--"+sessionFileFlag, directory, "erasure", "list"),
		append(serverArgs(), "--"+adminUserFlag, "owner", "--"+adminPasswordFlag, "-", "erasure", "list"),
	} {
		if result := runCommand(t, transport, nil, args...); result.err == nil {
			t.Fatalf("expected a local rejection for %v", args)
		}
	}
}

func TestSessionFileRejectsUnsafeAndUnwritablePaths(t *testing.T) {
	transport := adminTransport(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == loginPath {
			return cookieResponse(http.StatusOK, `{"code":200,"message":"success","data":{}}`, sampleCookie), nil
		}
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"jobs":[]}}`), nil
	})
	password := writeTokenFile(t, 0o600, "secret")
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("write empty session file: %v", err)
	}
	directory := t.TempDir()
	for _, path := range []string{directory, empty, filepath.Join(directory, "missing", "session")} {
		result := runCommand(t, transport, nil,
			append(erasureArgs(password, "--"+sessionFileFlag, path), "erasure", "list")...)
		if result.err == nil {
			t.Fatalf("expected a session file error for %s", path)
		}
	}
}
