package beatcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteResponseUnwrapsEnvelope(t *testing.T) {
	var output strings.Builder
	body := `{"code":200,"message":"success","data":{"status":"ok"}}`
	if err := writeResponse(&output, []byte(body), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := output.String(); got != "{\n  \"status\": \"ok\"\n}\n" {
		t.Fatalf("expected unwrapped data, got %q", got)
	}
}

func TestWriteResponseKeepsRawEnvelope(t *testing.T) {
	var output strings.Builder
	body := `{"code":200,"message":"success","data":[1]}`
	if err := writeResponse(&output, []byte(body), true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), `"code": 200`) || !strings.Contains(output.String(), `"data": [`) {
		t.Fatalf("expected the raw envelope, got %q", output.String())
	}
}

func TestWriteResponseFallsBackToOriginalBody(t *testing.T) {
	cases := []struct{ body, want string }{
		{body: `{"code":204,"message":"success"}`, want: "{\n  \"code\": 204,\n  \"message\": \"success\"\n}\n"},
		{body: `{"code":200,"message":"success","data":null}`, want: "{\n  \"code\": 200,\n  \"message\": \"success\",\n  \"data\": null\n}\n"},
		{body: `<html>proxy error</html>`, want: "<html>proxy error</html>\n"},
		{body: `{"code":200`, want: "{\"code\":200\n"},
	}
	for _, scenario := range cases {
		var output strings.Builder
		if err := writeResponse(&output, []byte(scenario.body), false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if output.String() != scenario.want {
			t.Fatalf("expected %q, got %q", scenario.want, output.String())
		}
	}
}

func TestWritePayloadHandlesEmptyAndFailingWriters(t *testing.T) {
	var output strings.Builder
	if err := writePayload(&output, []byte("   \n")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.String() != "" {
		t.Fatalf("expected no output for an empty payload, got %q", output.String())
	}
	if err := writePayload(failingWriter{}, []byte(`{"a":1}`)); err == nil {
		t.Fatal("expected a write error for JSON output")
	}
	if err := writePayload(failingWriter{}, []byte("plain")); err == nil {
		t.Fatal("expected a write error for passthrough output")
	}
}

func TestCreateOutputRefusesOverwriteUnlessForced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.zip")
	file, err := createOutput(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 output, got %o", info.Mode().Perm())
	}
	if _, err := createOutput(path, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected an overwrite guard, got %v", err)
	}
	forced, err := createOutput(path, true)
	if err != nil {
		t.Fatalf("unexpected forced error: %v", err)
	}
	if err := forced.Close(); err != nil {
		t.Fatalf("close forced file: %v", err)
	}
	if _, err := createOutput(filepath.Join(t.TempDir(), "missing", "backup.zip"), false); err == nil {
		t.Fatal("expected a create error for a missing directory")
	}
}

func TestCloseDownloadRemovesPartialFiles(t *testing.T) {
	directory := t.TempDir()
	partial := filepath.Join(directory, "partial.zip")
	file, err := createOutput(partial, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := closeDownload(file, partial, errors.New("download failed")); err == nil {
		t.Fatal("expected the download error to surface")
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the partial file to be removed, got %v", err)
	}
	if err := removePartial(partial, errors.New("download failed")); err == nil {
		t.Fatal("expected the original cause to be preserved")
	}
	complete := filepath.Join(directory, "complete.zip")
	kept, err := createOutput(complete, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := closeDownload(kept, complete, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(complete); err != nil {
		t.Fatalf("expected the completed file to remain: %v", err)
	}
}
