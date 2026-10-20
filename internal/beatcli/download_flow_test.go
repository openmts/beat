package beatcli

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type brokenBody struct{ sent bool }

func (body *brokenBody) Read(payload []byte) (int, error) {
	if body.sent {
		return 0, errors.New("stream interrupted")
	}
	body.sent = true
	return copy(payload, "partial"), nil
}

func (body *brokenBody) Close() error { return nil }

func TestBackupsDownloadWritesFile(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("archive"))}, nil
	}}
	base := append(serverArgs(), tokenArgs(t)...)
	path := filepath.Join(t.TempDir(), "backup.zip")
	args := append(append([]string{}, base...), "backups", "download", "b1", "--output", path)
	if result := runCommand(t, transport, nil, args...); result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	payload, err := os.ReadFile(path)
	if err != nil || string(payload) != "archive" {
		t.Fatalf("unexpected download: %q err=%v", payload, err)
	}
	if transport.requests[0].URL.Path != backupsPath+"/b1/download" {
		t.Fatalf("unexpected download path: %s", transport.requests[0].URL.Path)
	}
	again := runCommand(t, transport, nil, args...)
	if again.err == nil || !strings.Contains(again.err.Error(), "--force") {
		t.Fatalf("expected an overwrite guard, got %v", again.err)
	}
	forced := append(append([]string{}, args...), "--force")
	result := runCommand(t, transport, nil, forced...)
	if result.err != nil {
		t.Fatalf("unexpected forced error: %v", result.err)
	}
	if !strings.Contains(result.stderr, "wrote 7 bytes") {
		t.Fatalf("expected a stderr summary, got %q", result.stderr)
	}
	stdout := runCommand(t, transport, nil, append(append([]string{}, base...), "backups", "download", "b1", "--output", "-")...)
	if stdout.err != nil || stdout.stdout != "archive" {
		t.Fatalf("expected archive on stdout, got %q err=%v", stdout.stdout, stdout.err)
	}
	if result := runCommand(t, transport, nil, append(append([]string{}, base...), "backups", "download")...); result.err == nil {
		t.Fatal("expected a missing backup id error")
	}
}

func TestBackupsDownloadRemovesPartialFile(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: &brokenBody{}}, nil
	}}
	path := filepath.Join(t.TempDir(), "partial.zip")
	args := append(append(serverArgs(), tokenArgs(t)...), "backups", "download", "b1", "--output", path)
	if result := runCommand(t, transport, nil, args...); result.err == nil {
		t.Fatal("expected a download failure")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the partial file to be removed, got %v", err)
	}
}
