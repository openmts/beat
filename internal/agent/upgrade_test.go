package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	beatv1 "beat/internal/protocol/beatv1"
	"beat/internal/version"
)

func TestValidateUpgradeInstruction(t *testing.T) {
	payload := []byte("agent-binary")
	digest := sha256.Sum256(payload)
	upgrade := &beatv1.AgentUpgrade{
		OperationId: "op-1", TargetVersion: "9.9.9", Arch: runtime.GOARCH,
		DownloadUrl: "https://github.com/" + version.Repository + "/releases/download/v9.9.9/beat-agent-linux-" + runtime.GOARCH,
		Sha256:      hex.EncodeToString(digest[:]), Size: int64(len(payload)),
	}
	if err := validateUpgradeInstruction(upgrade); err != nil {
		t.Fatalf("valid instruction rejected: %v", err)
	}
	upgrade.DownloadUrl = "https://evil.example.com/" + version.Repository + "/releases/download/v9.9.9/beat-agent-linux-" + runtime.GOARCH
	if err := validateUpgradeInstruction(upgrade); err == nil {
		t.Fatal("cross-origin upgrade URL was accepted")
	}
	upgrade.DownloadUrl = "https://github.com/" + version.Repository + "/releases/download/v9.9.9/beat-agent-linux-" + runtime.GOARCH
	upgrade.TargetVersion = "0.0.1"
	if err := validateUpgradeInstruction(upgrade); err == nil {
		t.Fatal("downgrade was accepted")
	}
	upgrade.TargetVersion = "9.9.9"
	upgrade.DownloadUrl = "http://github.com/" + version.Repository + "/releases/download/v9.9.9/beat-agent-linux-" + runtime.GOARCH
	if err := validateUpgradeInstruction(upgrade); err == nil {
		t.Fatal("non-HTTPS upgrade URL was accepted")
	}
}

func TestDownloadUpgradeVerifiesChecksumAndSize(t *testing.T) {
	payload := []byte("new-agent-binary")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/beat-agent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	runtimeAgent := &Runtime{cfg: RuntimeConfig{ServerURL: server.URL}}
	upgrade := &beatv1.AgentUpgrade{
		OperationId: "op-download", TargetVersion: "9.9.9", Arch: runtime.GOARCH,
		DownloadUrl: server.URL + "/beat-agent",
		Sha256:      hex.EncodeToString(digest[:]), Size: int64(len(payload)), TimeoutSeconds: 60,
	}
	path, err := runtimeAgent.downloadUpgrade(context.Background(), upgrade, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Fatalf("downloaded body = %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %o, want 750", info.Mode().Perm())
	}
	upgrade.Sha256 = strings.Repeat("0", 64)
	if _, err := runtimeAgent.downloadUpgrade(context.Background(), upgrade, t.TempDir()); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

func TestReplaceAgentBinary(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "beat-agent")
	if err := os.WriteFile(executable, []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, ".beat-agent-upgrade-test")
	if err := os.WriteFile(staged, []byte("new"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := replaceAgentBinary(executable, staged); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("installed binary = %q", body)
	}
	previous, err := os.ReadFile(executable + ".previous")
	if err != nil {
		t.Fatal(err)
	}
	if string(previous) != "old" {
		t.Fatalf("backup binary = %q", previous)
	}
	if allowedUpgradeTarget(executable) {
		t.Fatalf("temporary path %s must not be upgradeable", executable)
	}
	if !allowedUpgradeTarget("/usr/local/bin/beat-agent") {
		t.Fatal("standard installed path should be upgradeable")
	}
}
