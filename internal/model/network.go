package model

import (
	"errors"
	"math"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	NetworkProbeICMP = "icmp"
	NetworkProbeTCP  = "tcp"
	NetworkProbeHTTP = "http"

	IPFamilyAuto = "auto"
	IPFamilyIPv4 = "ipv4"
	IPFamilyIPv6 = "ipv6"

	MinNetworkInterval = 10 * time.Second
	MaxNetworkInterval = 24 * time.Hour
	MinNetworkTimeout  = 100 * time.Millisecond
	MaxNetworkTimeout  = 30 * time.Second
)

var networkErrorCodes = map[string]struct{}{
	"none": {}, "dns": {}, "timeout": {}, "permission": {},
	"connection_refused": {}, "network_unreachable": {}, "tls": {},
	"http_status": {}, "invalid_target": {}, "protocol": {}, "io": {}, "internal": {},
}

type NetworkTask struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Type                string        `json:"type"`
	Target              string        `json:"target"`
	IPFamily            string        `json:"ip_family"`
	IntervalSeconds     int           `json:"interval_seconds"`
	TimeoutMilliseconds int           `json:"timeout_milliseconds"`
	AllNodes            bool          `json:"all_nodes"`
	Enabled             bool          `json:"enabled"`
	IsPublic            bool          `json:"is_public"`
	SortOrder           int           `json:"sort_order"`
	Nodes               []NetworkNode `json:"nodes"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

type NetworkNode struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

type NetworkAssignment struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Type                string `json:"type"`
	Target              string `json:"target"`
	IPFamily            string `json:"ip_family"`
	IntervalSeconds     int    `json:"interval_seconds"`
	TimeoutMilliseconds int    `json:"timeout_milliseconds"`
}

type NetworkProbeResult struct {
	TaskID     string    `json:"task_id"`
	FinishedAt time.Time `json:"finished_at"`
	LatencyMS  float64   `json:"latency_ms"`
	Success    bool      `json:"success"`
	StatusCode int       `json:"status_code"`
	ErrorCode  string    `json:"error_code"`
}

func (task NetworkTask) Validate() error {
	name := strings.TrimSpace(task.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return errors.New("network task name must contain 1 to 100 characters")
	}
	if len(task.Target) == 0 || len(task.Target) > 2048 {
		return errors.New("network task target must contain 1 to 2048 bytes")
	}
	if !validNetworkProbeType(task.Type) {
		return errors.New("network task type is invalid")
	}
	if !validIPFamily(task.IPFamily) {
		return errors.New("network task IP family is invalid")
	}
	interval := time.Duration(task.IntervalSeconds) * time.Second
	timeout := time.Duration(task.TimeoutMilliseconds) * time.Millisecond
	if interval < MinNetworkInterval || interval > MaxNetworkInterval {
		return errors.New("network task interval is out of range")
	}
	if timeout < MinNetworkTimeout || timeout > MaxNetworkTimeout || timeout > interval {
		return errors.New("network task timeout is out of range")
	}
	if task.SortOrder < 0 {
		return errors.New("network task sort order must not be negative")
	}
	return validateNetworkTarget(task.Type, task.Target)
}

func (result NetworkProbeResult) Validate() error {
	if strings.TrimSpace(result.TaskID) == "" {
		return errors.New("network probe task ID is required")
	}
	if result.FinishedAt.IsZero() {
		return errors.New("network probe finish time is required")
	}
	if math.IsNaN(result.LatencyMS) || math.IsInf(result.LatencyMS, 0) || result.LatencyMS < 0 {
		return errors.New("network probe latency is invalid")
	}
	if result.StatusCode < 0 || result.StatusCode > 999 {
		return errors.New("network probe status code is invalid")
	}
	if _, ok := networkErrorCodes[result.ErrorCode]; !ok {
		return errors.New("network probe error code is invalid")
	}
	if result.Success && result.ErrorCode != "none" {
		return errors.New("successful network probe must use the none error code")
	}
	return nil
}

func validNetworkProbeType(probeType string) bool {
	return probeType == NetworkProbeICMP || probeType == NetworkProbeTCP || probeType == NetworkProbeHTTP
}

func validIPFamily(family string) bool {
	return family == IPFamilyAuto || family == IPFamilyIPv4 || family == IPFamilyIPv6
}

func validateNetworkTarget(probeType, target string) error {
	switch probeType {
	case NetworkProbeICMP:
		if strings.TrimSpace(target) != target || strings.ContainsAny(target, "/:?#@") {
			return errors.New("ICMP target must be an IP address or hostname")
		}
	case NetworkProbeTCP:
		host, port, err := net.SplitHostPort(target)
		if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
			return errors.New("TCP target must use host:port format")
		}
	case NetworkProbeHTTP:
		parsed, err := url.ParseRequestURI(target)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("HTTP target must be an absolute HTTP or HTTPS URL")
		}
		if parsed.User != nil || parsed.Fragment != "" {
			return errors.New("HTTP target must not contain credentials or fragments")
		}
	}
	return nil
}
