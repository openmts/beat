package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"beat/internal/auth"
	"beat/internal/config"
	"beat/internal/domain"
	"beat/internal/platform/sqlite"
	"beat/internal/version"
)

func TestAgentUpgradeRoutes(t *testing.T) {
	previousVersion := version.Version
	version.Version = "0.2.0"
	t.Cleanup(func() { version.Version = previousVersion })

	store, err := sqlite.Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	binaryDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binaryDir, "beat-agent-linux-amd64"), []byte("agent-amd64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binaryDir, "beat-agent-linux-arm64"), []byte("agent-arm64"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.UpsertNode(ctx, domain.Report{
		AgentID: "agent-old", Hostname: "edge-old", Arch: "amd64", AgentVersion: "0.1.0",
		IntervalSeconds: 15, Metrics: domain.Metrics{Timestamp: now, CPUUsage: 10},
	}, now); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg:    config.Config{Listen: ":9099", EnrollmentTokenTTL: 10 * time.Minute},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)), store: store,
		authManager: manager, agentSessions: newAgentSessionRegistry(), agentBinaryDir: binaryDir,
	}
	router := s.routes()
	login := serveRequest(router, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"admin"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	var payload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	cookie := login.Result().Cookies()[0]
	cookieValue := cookie.Name + "=" + cookie.Value

	overview := serveRequest(router, http.MethodGet, "/api/v1/agent-upgrades/overview", cookieValue, "")
	if overview.Code != http.StatusOK || !strings.Contains(overview.Body.String(), `"upgradable":1`) || !strings.Contains(overview.Body.String(), `"targetVersion":"0.2.0"`) {
		t.Fatalf("overview = %d: %s", overview.Code, overview.Body.String())
	}
	created := serveAdminRequest(router, http.MethodPost, "/api/v1/agent-upgrades", cookieValue, payload.CSRFToken, `{"includeOffline":true}`)
	if created.Code != http.StatusAccepted || !strings.Contains(created.Body.String(), `"agentId":"agent-old"`) || !strings.Contains(created.Body.String(), `"status":"queued"`) {
		t.Fatalf("create upgrade = %d: %s", created.Code, created.Body.String())
	}
	var response struct {
		Batch domain.AgentUpgradeBatch `json:"batch"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Batch.Total != 1 || response.Batch.TargetVersion != "0.2.0" {
		t.Fatalf("unexpected batch: %+v", response.Batch)
	}
	detail := serveRequest(router, http.MethodGet, "/api/v1/agent-upgrades/"+response.Batch.ID, cookieValue, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"nodeName":"edge-old"`) {
		t.Fatalf("batch detail = %d: %s", detail.Code, detail.Body.String())
	}
	duplicate := serveAdminRequest(router, http.MethodPost, "/api/v1/agent-upgrades", cookieValue, payload.CSRFToken, `{"includeOffline":true}`)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "upgrade_in_progress") {
		t.Fatalf("duplicate upgrade = %d: %s", duplicate.Code, duplicate.Body.String())
	}
	unauthorized := serveRequest(router, http.MethodGet, "/api/v1/agent-upgrades/overview", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous overview = %d, want 401", unauthorized.Code)
	}
}
