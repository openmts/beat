package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestNetworkHTTPClient(t *testing.T) {
	reported := make(chan []model.NetworkProbeResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case networkAssignmentsPath:
			if r.URL.RawQuery != "" {
				t.Errorf("assignment query = %q, want empty", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"expires_at": time.Now().Add(time.Minute),
				"tasks":      []model.NetworkAssignment{{ID: "task", Name: "Task", Type: "tcp"}},
			}})
		case networkResultsPath:
			var body struct {
				Results []model.NetworkProbeResult `json:"results"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode results: %v", err)
			}
			reported <- body.Results
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewNetworkHTTPClient(Config{ServerURL: server.URL, AgentToken: "token", NodeName: "node-one"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	set, err := client.FetchAssignments(t.Context())
	if err != nil || len(set.Tasks) != 1 || set.Tasks[0].Name != "Task" {
		t.Fatalf("assignments = %#v, error = %v", set, err)
	}
	results := []model.NetworkProbeResult{{TaskID: "task", FinishedAt: time.Now(), ErrorCode: "none"}}
	if err := client.ReportResults(t.Context(), results); err != nil {
		t.Fatalf("report results: %v", err)
	}
	if got := <-reported; len(got) != 1 || got[0].TaskID != "task" {
		t.Fatalf("reported = %#v", got)
	}
}

func TestNetworkHTTPClientErrors(t *testing.T) {
	if _, err := NewNetworkHTTPClient(Config{ServerURL: "://bad"}); err == nil {
		t.Fatal("expected invalid URL error")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, strings.Repeat("x", maxErrorBody+10), http.StatusBadGateway)
	}))
	defer server.Close()
	client, err := NewNetworkHTTPClient(Config{ServerURL: server.URL, AgentToken: "token", NodeName: "node"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.FetchAssignments(t.Context()); err == nil {
		t.Fatal("expected assignment response error")
	}
	if err := client.ReportResults(t.Context(), []model.NetworkProbeResult{{TaskID: "task"}}); err == nil {
		t.Fatal("expected result response error")
	}
}

func TestNetworkHTTPClientTransportAndDecodeErrors(t *testing.T) {
	invalidJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	client, err := NewNetworkHTTPClient(Config{ServerURL: invalidJSON.URL, NodeName: "node"})
	if err != nil {
		t.Fatalf("new decode client: %v", err)
	}
	if _, err := client.FetchAssignments(t.Context()); err == nil {
		t.Fatal("expected assignment decode error")
	}
	invalidJSON.Close()
	if _, err := client.FetchAssignments(t.Context()); err == nil {
		t.Fatal("expected assignment transport error")
	}
	if err := client.ReportResults(t.Context(), nil); err == nil {
		t.Fatal("expected result transport error")
	}

	response := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(networkErrorReader{}),
	}
	if err := networkResponseError(response, "network test"); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("response read error = %v", err)
	}
}

type networkErrorReader struct{}

func (networkErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
