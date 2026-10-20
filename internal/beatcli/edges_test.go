package beatcli

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewClientBuildsBoundedDefaultClient(t *testing.T) {
	client := NewClient(testBase(t), "beat_service_v1_test", nil)
	if client.client.Timeout != DefaultTimeout {
		t.Fatalf("expected the default timeout, got %s", client.client.Timeout)
	}
	if client.client.CheckRedirect == nil {
		t.Fatal("expected a redirect policy")
	}
}

func TestReadBoundedReportsReadFailures(t *testing.T) {
	if _, err := readBounded(&brokenBody{}); err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("expected a read error, got %v", err)
	}
}

func TestDecodeAPIErrorFallsBackWhenBodyReadFails(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: &brokenBody{}}, nil
	}}
	_, err := testClient(t, transport).Get(t.Context(), runtimePath, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Status != http.StatusBadGateway || apiErr.Message != "" || !strings.Contains(apiErr.Error(), "Bad Gateway") {
		t.Fatalf("unexpected fallback error: %+v %q", apiErr, apiErr.Error())
	}
}

func TestClientReportsRequestConstructionFailures(t *testing.T) {
	client := testClient(t, &stubTransport{})
	if _, err := client.do(t.Context(), "bad\nmethod", runtimePath, nil); err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("expected a build error, got %v", err)
	}
}

func TestDownloadSurfacesServerErrors(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusForbidden, `{"code":403,"message":"service token is not permitted"}`), nil
	}}
	if _, err := testClient(t, transport).Download(t.Context(), backupsPath+"/b1/download", io.Discard); err == nil {
		t.Fatal("expected the download to fail")
	}
}

func TestBoundRetryAfterRejectsNonPositive(t *testing.T) {
	if got := boundRetryAfter(0); got != 0 {
		t.Fatalf("expected zero, got %s", got)
	}
	if got := boundRetryAfter(-time.Second); got != 0 {
		t.Fatalf("expected zero, got %s", got)
	}
	if got := boundRetryAfter(time.Hour); got != time.Hour {
		t.Fatalf("expected one hour, got %s", got)
	}
}

func TestSessionHelpersReportTransportErrors(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusForbidden, `{"code":403,"message":"service token is not permitted"}`), nil
	}}
	session := &session{client: testClient(t, transport), output: io.Discard, summary: io.Discard}
	if err := session.get(t.Context(), runtimePath, nil); err == nil {
		t.Fatal("expected the read to fail")
	}
	if err := session.post(t.Context(), backupsPath); err == nil {
		t.Fatal("expected the write to fail")
	}
}

func TestNormalizeOptionsFillsDefaults(t *testing.T) {
	options := normalizeOptions(Options{})
	if options.Output == nil || options.Errors == nil || options.Stdin == nil || options.Getenv == nil {
		t.Fatalf("expected non-nil defaults, got %+v", options)
	}
	custom := func(string) string { return "custom" }
	kept := normalizeOptions(Options{Output: io.Discard, Errors: io.Discard, Stdin: strings.NewReader(""), Getenv: custom})
	if kept.Output != io.Discard || kept.Errors != io.Discard || kept.Getenv == nil {
		t.Fatalf("expected injected values to survive, got %+v", kept)
	}
}

func TestReportHandlesMissingAndFailingSummaries(t *testing.T) {
	if err := (&session{}).report(3, "file"); err != nil {
		t.Fatalf("unexpected error without a summary writer: %v", err)
	}
	writer := &strings.Builder{}
	if err := (&session{summary: writer}).report(3, "file"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(writer.String(), "wrote 3 bytes to file") {
		t.Fatalf("unexpected summary: %q", writer.String())
	}
	if err := (&session{summary: failingWriter{}}).report(3, "file"); err == nil {
		t.Fatal("expected the summary failure to surface")
	}
}

func TestCloseDownloadReportsCloseFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.zip")
	file, err := createOutput(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	if err := closeDownload(file, path, nil); err == nil || !strings.Contains(err.Error(), "close output file") {
		t.Fatalf("expected a close error, got %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the file to be removed, got %v", err)
	}
}

func TestRemovePartialReportsRemovalFailure(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := removePartial(nested, errors.New("download failed"))
	if err == nil || !strings.Contains(err.Error(), "partial file remains") {
		t.Fatalf("expected a removal failure, got %v", err)
	}
}
