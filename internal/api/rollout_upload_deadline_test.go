package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
)

func TestSlowRolloutUploadExtendsServerWriteDeadlineForReceipt(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	release := rolloutResponse[agentrollout.Release](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: rolloutTestOrigin, body: f.signed}))
	server := httptest.NewUnstartedServer(middleware.Logging(f.router.ServeHandler()))
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	request, err := http.NewRequest("PUT", server.URL+"/api/v1/admin/agent-releases/"+release.ID+"/artifacts/linux-amd64", reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", server.URL)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: f.owner.Token})
	produced := make(chan error, 1)
	finished := make(chan struct{})
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close(); <-finished })
	go func() {
		defer close(finished)
		_, firstErr := writer.Write(f.artifact[:1])
		time.Sleep(100 * time.Millisecond)
		_, restErr := writer.Write(f.artifact[1:])
		closeErr := writer.Close()
		produced <- errors.Join(firstErr, restErr, closeErr)
	}()
	client := server.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("slow upload lost its completion receipt", err)
	}
	defer func() { _ = response.Body.Close() }()
	var receipt struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&receipt); err != nil {
		t.Fatal("decode completion receipt", err)
	}
	if response.StatusCode != 200 || receipt.Code != 200 || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(response.StatusCode, receipt, response.Header)
	}
	if err := <-produced; err != nil {
		t.Fatal(err)
	}
	var state string
	if err := f.db.DB.QueryRow("SELECT state FROM agent_release_artifacts WHERE release_id=?", release.ID).Scan(&state); err != nil || state != "ready" {
		t.Fatal(state, err)
	}
}
