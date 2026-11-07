package model

import (
	"errors"
	"math"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	NetworkProbeICMP = "icmp"
	NetworkProbeTCP  = "tcp"
	NetworkProbeHTTP = "http"

	NetworkDirectionAgentToTarget = "agent_to_target"
	NetworkDirectionServerToAgent = "server_to_agent"

	IPFamilyAuto = "auto"
	IPFamilyIPv4 = "ipv4"
	IPFamilyIPv6 = "ipv6"

	DefaultNetworkPacketCount = 5
	MaxNetworkPacketCount     = 20
	MinNetworkInterval        = 10 * time.Second
	MaxNetworkInterval        = 24 * time.Hour
	MinNetworkTimeout         = 100 * time.Millisecond
	MaxNetworkTimeout         = 30 * time.Second
)

var networkErrorCodes = map[string]struct{}{
	"none": {}, "dns": {}, "timeout": {}, "permission": {},
	"connection_refused": {}, "network_unreachable": {}, "tls": {},
	"http_status": {}, "invalid_target": {}, "protocol": {}, "io": {}, "internal": {},
}

type NetworkTask struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Direction           string        `json:"direction"`
	Type                string        `json:"type"`
	Target              string        `json:"target"`
	IPFamily            string        `json:"ip_family"`
	IntervalSeconds     int           `json:"interval_seconds"`
	TimeoutMilliseconds int           `json:"timeout_milliseconds"`
	PacketCount         int           `json:"packet_count"`
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
	Direction           string `json:"direction"`
	Type                string `json:"type"`
	Target              string `json:"target"`
	IPFamily            string `json:"ip_family"`
	IntervalSeconds     int    `json:"interval_seconds"`
	TimeoutMilliseconds int    `json:"timeout_milliseconds"`
	PacketCount         int    `json:"packet_count"`
}

type NetworkProbeTarget struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	Alias  string `json:"alias"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type ServerNetworkTask struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Type                string               `json:"type"`
	Target              string               `json:"target"`
	IPFamily            string               `json:"ip_family"`
	IntervalSeconds     int                  `json:"interval_seconds"`
	TimeoutMilliseconds int                  `json:"timeout_milliseconds"`
	PacketCount         int                  `json:"packet_count"`
	Targets             []NetworkProbeTarget `json:"targets"`
}

type NetworkProbeResult struct {
	TaskID          string    `json:"task_id"`
	FinishedAt      time.Time `json:"finished_at"`
	LatencyMS       float64   `json:"latency_ms"`
	Success         bool      `json:"success"`
	StatusCode      int       `json:"status_code"`
	ErrorCode       string    `json:"error_code"`
	SentPackets     int       `json:"sent_packets"`
	ReceivedPackets int       `json:"received_packets"`
	LossPercent     *float64  `json:"loss_percent"`
	JitterMS        *float64  `json:"jitter_ms"`
	MinLatencyMS    *float64  `json:"min_latency_ms"`
	MaxLatencyMS    *float64  `json:"max_latency_ms"`
}

// Normalize 为创建和更新补上默认方向、默认包数与服务端探测目标模板。
// 现存任务通过数据库默认值保持 agent_to_target；新 ICMP 任务默认发送 5 个包。
func (task *NetworkTask) Normalize() {
	if task.Direction == "" {
		task.Direction = NetworkDirectionAgentToTarget
	}
	if task.PacketCount <= 0 {
		if task.Type == NetworkProbeICMP {
			task.PacketCount = DefaultNetworkPacketCount
		} else {
			task.PacketCount = 1
		}
	}
	if task.Type != NetworkProbeICMP && task.PacketCount > 1 {
		task.PacketCount = 1
	}
	if task.Direction != NetworkDirectionServerToAgent || strings.TrimSpace(task.Target) != "" {
		return
	}
	switch task.Type {
	case NetworkProbeICMP:
		task.Target = "{{host}}"
	case NetworkProbeTCP:
		task.Target = "{{host_port}}"
	case NetworkProbeHTTP:
		task.Target = "http://{{host}}/"
	}
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
	if !validNetworkDirection(task.Direction) {
		return errors.New("network task direction is invalid")
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
	if task.PacketCount < 1 || task.PacketCount > MaxNetworkPacketCount {
		return errors.New("network task packet count is out of range")
	}
	if task.Direction == NetworkDirectionServerToAgent {
		return validateServerNetworkTarget(task.Type, task.Target)
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
	if result.SentPackets < 0 || result.ReceivedPackets < 0 ||
		result.ReceivedPackets > result.SentPackets {
		return errors.New("network probe packet counts are invalid")
	}
	if err := validateOptionalNonNegative(result.LossPercent, "loss"); err != nil {
		return err
	}
	if result.LossPercent != nil && *result.LossPercent > 100 {
		return errors.New("network probe loss is invalid")
	}
	if err := validateOptionalNonNegative(result.JitterMS, "jitter"); err != nil {
		return err
	}
	if err := validateOptionalNonNegative(result.MinLatencyMS, "minimum latency"); err != nil {
		return err
	}
	if err := validateOptionalNonNegative(result.MaxLatencyMS, "maximum latency"); err != nil {
		return err
	}
	if result.MinLatencyMS != nil && result.MaxLatencyMS != nil &&
		*result.MinLatencyMS > *result.MaxLatencyMS {
		return errors.New("network probe latency range is invalid")
	}
	return nil
}

func validNetworkProbeType(probeType string) bool {
	return probeType == NetworkProbeICMP || probeType == NetworkProbeTCP || probeType == NetworkProbeHTTP
}

func validNetworkDirection(direction string) bool {
	return direction == NetworkDirectionAgentToTarget || direction == NetworkDirectionServerToAgent
}

func validIPFamily(family string) bool {
	return family == IPFamilyAuto || family == IPFamilyIPv4 || family == IPFamilyIPv6
}

func validateNetworkTarget(probeType, target string) error {
	switch probeType {
	case NetworkProbeICMP:
		if strings.TrimSpace(target) != target || strings.ContainsAny(target, "/?#@") {
			return errors.New("ICMP target must be an IP address or hostname")
		}
		if address, err := netip.ParseAddr(target); err == nil {
			if address.Is4() || address.Is6() {
				return nil
			}
		}
		if strings.Contains(target, ":") {
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
