package handler

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

func declarativeFixtureAPI(w http.ResponseWriter, r *http.Request) {
	if themeMetricFixtureAPI(w, r) {
		return
	}
	path := r.URL.Path
	if path == "/api/v1/auth/session" && r.Header.Get("X-Beat-Fixture-Owner") == "" {
		JSONError(w, http.StatusUnauthorized, "anonymous fixture")
		return
	}
	if path == "/api/v1/settings/site" {
		JSONResponse(w, http.StatusOK, map[string]any{"site_title": "Beat · Example fleet", "site_description": "Public infrastructure overview", "default_theme": "light", "public_address_mode": "hidden", "show_network_quality": false})
		return
	}
	if path == "/api/v1/groups" {
		JSONResponse(w, http.StatusOK, []any{map[string]any{"id": "production", "name": "Production", "sort_order": 0}, map[string]any{"id": "staging", "name": "Staging", "sort_order": 1}})
		return
	}
	if path == "/api/v1/nodes" {
		nodes := declarativeFixtureNodes()
		if r.Header.Get("X-Beat-Fixture-Owner") != "" {
			nodes = append(nodes, map[string]any{"id": "private", "name": "PRIVATE_NODE", "group_id": "production", "status": "online", "is_public": false})
		}
		JSONResponse(w, http.StatusOK, nodes)
		return
	}
	if strings.HasPrefix(path, "/api/v1/nodes/") {
		declarativeNodeAPI(w, r)
		return
	}
	var counter atomic.Int64
	browserFixtureAPI(w, r, &counter)
}

func declarativeFixtureNodes() []map[string]any {
	names := []string{"Frankfurt edge", "Tokyo edge", "Singapore edge", "Ashburn worker", "Seattle standby", "Canary node"}
	cpus := []float64{42.7, 12.4, 79.2, 24.5, 0, 0}
	nodes := make([]map[string]any, 0, len(names))
	for index, name := range names {
		group := "production"
		if index >= 4 {
			group = "staging"
		}
		status := "online"
		if index == 4 {
			status = "offline"
		}
		metrics := map[string]any{"cpu": cpus[index], "memory": 35.5 + float64(index)*7, "disk": 23.4 + float64(index)*8, "swap": 0,
			"cpu_total": 8, "cpu_used": cpus[index] * 0.08, "memory_total": 16 << 30, "memory_used": 6 << 30, "disk_total": 500 << 30, "disk_used": 150 << 30,
			"net_recv": 1536000, "net_sent": 524288, "uptime": 172890, "load1": 1.3, "load5": 1.1, "load15": 0.8, "processes": 156,
			"tcp_connections": 84, "udp_connections": 12, "private_sample": 999}
		if index == 5 {
			metrics = map[string]any{}
		}
		var telemetryAt any = time.Now().UTC().Format(time.RFC3339)
		var telemetryAge any = 0
		telemetryState := "fresh"
		if index == 4 {
			telemetryAt, telemetryAge, telemetryState = time.Now().Add(-10*time.Minute).UTC().Format(time.RFC3339), 600, "stale"
		}
		if index == 5 {
			telemetryAt, telemetryAge, telemetryState = nil, nil, "unavailable"
		}
		nodes = append(nodes, map[string]any{"id": "node-" + strconv.Itoa(index), "name": name, "alias": "", "group_id": group, "status": status, "is_public": true,
			"telemetry_at": telemetryAt, "telemetry_age_seconds": telemetryAge, "telemetry_state": telemetryState,
			"host": "203.0.113.18", "private_remark": "PRIVATE_REMARK", "ssh_public_key": "PRIVATE_KEY", "agent_token": "PRIVATE_TOKEN",
			"tags": []string{"linux", "edge"}, "public_remark": "Managed infrastructure · example data", "metrics": metrics, "last_seen": time.Now().UTC().Format(time.RFC3339),
			"cpu_model": "AMD EPYC 7763", "os": "linux", "platform": "Ubuntu", "os_version": "24.04", "kernel": "6.8.0", "arch": "amd64", "virtualization": "kvm", "agent_version": "1.0.0"})
	}
	return nodes
}

func declarativeNodeAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/"), "/")
	if len(parts) > 1 && parts[1] == "custom-metrics" {
		JSONResponse(w, http.StatusOK, []any{})
		return
	}
	if len(parts) > 1 && parts[1] == "metrics" {
		JSONResponse(w, http.StatusOK, declarativeHistory())
		return
	}
	for _, node := range declarativeFixtureNodes() {
		if node["id"] == parts[0] {
			JSONResponse(w, http.StatusOK, node)
			return
		}
	}
	JSONError(w, http.StatusNotFound, "node not found")
}

func declarativeHistory() map[string][]map[string]any {
	history := make(map[string][]map[string]any)
	for _, key := range []string{"cpu", "memory", "disk", "swap", "net_recv", "net_sent", "load1", "processes", "tcp_connections"} {
		for point := 0; point < 60; point++ {
			value := 40 + math.Sin(float64(point)/7)*15
			history[key] = append(history[key], map[string]any{"timestamp": time.Now().Unix() - int64(59-point)*60, "value": value})
		}
	}
	return history
}
