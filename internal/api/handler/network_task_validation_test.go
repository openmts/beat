package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNetworkTaskValidationReturnsSpecificReasons(t *testing.T) {
	fixture := setupNetworkHandler(t)
	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{
			name: "invalid ICMP target",
			payload: map[string]any{
				"name": "Probe", "type": "icmp", "target": "http://example.test",
				"ip_family": "auto", "interval_seconds": 60, "timeout_milliseconds": 1000,
				"all_nodes": true, "enabled": true, "is_public": true, "sort_order": 0,
			},
			want: "ICMP target must be an IP address or hostname",
		},
		{
			name: "missing explicit assignment",
			payload: map[string]any{
				"name": "Probe", "type": "icmp", "target": "127.0.0.1",
				"ip_family": "auto", "interval_seconds": 60, "timeout_milliseconds": 1000,
				"all_nodes": false, "node_ids": []string{}, "enabled": true, "is_public": true, "sort_order": 0,
			},
			want: "explicit network task assignment requires at least one node",
		},
		{
			name: "duplicate node assignment",
			payload: map[string]any{
				"name": "Probe", "type": "icmp", "target": "127.0.0.1",
				"ip_family": "auto", "interval_seconds": 60, "timeout_milliseconds": 1000,
				"all_nodes": false, "node_ids": []string{fixture.node.ID, fixture.node.ID},
				"enabled": true, "is_public": true, "sort_order": 0,
			},
			want: "network task node IDs must be unique",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatalf("encode payload: %v", err)
			}
			response := httptest.NewRecorder()
			fixture.handler.HandleCreateNetworkTask(response,
				networkHandlerRequest(http.MethodPost, "/tasks", string(body)))
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("status = %d, body = %s, want reason %q", response.Code, response.Body.String(), test.want)
			}
		})
	}
}

func TestNetworkTaskCreateAcceptsHTTPToTCPNormalizedTarget(t *testing.T) {
	fixture := setupNetworkHandler(t)
	payload := map[string]any{
		"name": "TCP after protocol switch", "type": "tcp",
		"target":    "gd-cm-v4.ip.zstaticcdn.com:80",
		"ip_family": "auto", "interval_seconds": 60, "timeout_milliseconds": 1000,
		"all_nodes": true, "enabled": true, "is_public": true, "sort_order": 0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	response := httptest.NewRecorder()
	fixture.handler.HandleCreateNetworkTask(response,
		networkHandlerRequest(http.MethodPost, "/tasks", string(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"target":"gd-cm-v4.ip.zstaticcdn.com:80"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestNetworkTaskUpdateReportsDirectionChange(t *testing.T) {
	fixture := setupNetworkHandler(t)
	created := createHandlerTask(t, fixture, true, true)
	payload := map[string]any{
		"name": created.Name, "direction": "server_to_agent", "type": "icmp", "target": "{{host}}",
		"target_mode": "single", "targets": []any{}, "ip_family": "auto",
		"interval_seconds": 60, "timeout_milliseconds": 1000, "packet_count": 5,
		"all_nodes": true, "enabled": true, "is_public": true, "sort_order": 0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	request := requestWithTaskID(http.MethodPut, created.ID, string(body))
	response := httptest.NewRecorder()
	fixture.handler.HandleUpdateNetworkTask(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "direction cannot be changed") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
