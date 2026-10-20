package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestUpdateHealthOnlyFollowsCompleteReportACK(t *testing.T) {
	reporter, err := NewHTTPReporter(Config{ServerURL: "https://beat.test", AgentToken: "private-token", NodeName: "fixture", AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sample := testReportSample(t, model.NodeMetrics{}, nil)
	serverAt := time.Now().UTC()
	accepted, failed := 0, 0
	reporter.WithReportObserver(func(at time.Time) {
		accepted++
		if !at.Equal(serverAt) {
			t.Error("server contact time altered")
		}
	}, func(error) { failed++ })
	for _, valid := range []bool{false, true} {
		ack := model.AgentReportACK{ProtocolVersion: model.AgentReportProtocol, SampleID: sample.SampleID, CapturedAt: sample.CapturedAt, ContactAccepted: true, MetricsAccepted: valid, ContactAt: &serverAt, SSHPublicKey: "public-key"}
		payload, err := json.Marshal(map[string]any{"data": ack})
		if err != nil {
			t.Fatal(err)
		}
		reporter.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
		})
		key, err := reporter.Report(t.Context(), sample)
		if valid {
			if err != nil || key != "public-key" {
				t.Fatal(key, err)
			}
		} else if err == nil {
			t.Fatal("incomplete ACK accepted")
		}
	}
	if accepted != 1 || failed != 1 {
		t.Fatal("invalid health callback counts", accepted, failed)
	}
}

func TestUpdateSettingsRemainLocalAndOptIn(t *testing.T) {
	for _, updates := range []string{`{"enabled":false}`, `{"enabled":true,"node_id":"node-id"}`, `{"enabled":true}`} {
		data := `{"server_url":"https://beat.test","agent_token":"private","node_name":"fixture","advertised_host":"127.0.0.1","ssh_port":22,"report_interval":"1s","updates":` + updates + `}`
		path := filepath.Join(t.TempDir(), "agent.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		config, err := LoadConfig(path)
		if updates == `{"enabled":true}` {
			if err == nil {
				t.Fatal("missing node identity accepted")
			}
			continue
		}
		if err != nil || config.Updates == nil {
			t.Fatal(config.Updates, err)
		}
	}
}
