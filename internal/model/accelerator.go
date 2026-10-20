package model

import "time"

const AcceleratorReportVersion = 1

const MaximumAccelerators = 32

var AcceleratorMetricKeys = []string{"gpu_utilization_percent", "gpu_memory_used_bytes", "gpu_memory_total_bytes", "gpu_temperature_celsius", "gpu_power_watts", "gpu_power_limit_watts"}

// AcceleratorReport 保留独立采集时间与cadence；主机心跳不得刷新缓存GPU样本时间。
type AcceleratorReport struct {
	InstallationEnabled bool                      `json:"installation_enabled"`
	PolicyRevision      int64                     `json:"policy_revision"`
	Version             int                       `json:"version"`
	SampleID            string                    `json:"sample_id"`
	CapturedAt          time.Time                 `json:"captured_at"`
	CadenceMS           int64                     `json:"cadence_ms"`
	CollectorVersion    string                    `json:"collector_version"`
	State               string                    `json:"state"`
	ErrorCode           string                    `json:"error_code,omitempty"`
	Capabilities        []AcceleratorCapability   `json:"capabilities"`
	Devices             []AcceleratorDeviceReport `json:"devices"`
}

type AcceleratorCapability struct {
	Vendor    string   `json:"vendor"`
	State     string   `json:"state"`
	Metrics   []string `json:"metrics"`
	ErrorCode string   `json:"error_code,omitempty"`
}

type AcceleratorDeviceReport struct {
	Fingerprint   string                   `json:"fingerprint"`
	Vendor        string                   `json:"vendor"`
	Model         string                   `json:"model"`
	DriverVersion string                   `json:"driver_version,omitempty"`
	Capabilities  []string                 `json:"capabilities"`
	Metrics       map[string]float64       `json:"metrics"`
	Errors        []AcceleratorMetricError `json:"errors,omitempty"`
}

type AcceleratorMetricError struct {
	Metric string `json:"metric"`
	Code   string `json:"code"`
}

// AcceleratorDevicePublic 不包含原始fingerprint、PCI地址、序列号、driver或collector输出。
type AcceleratorDevicePublic struct {
	DeviceID     string             `json:"device_id"`
	Ordinal      int                `json:"ordinal"`
	Vendor       string             `json:"vendor"`
	Model        string             `json:"model"`
	Capabilities []string           `json:"capabilities"`
	Metrics      map[string]float64 `json:"metrics"`
	CapturedAt   *time.Time         `json:"captured_at,omitempty"`
	CadenceMS    int64              `json:"cadence_ms"`
	Stale        bool               `json:"stale"`
}

type AcceleratorDeviceAdmin struct {
	AcceleratorDevicePublic
	Fingerprint   string     `json:"fingerprint"`
	DriverVersion string     `json:"driver_version,omitempty"`
	FirstSeenAt   time.Time  `json:"first_seen_at"`
	LastSeenAt    time.Time  `json:"last_seen_at"`
	InactiveAt    *time.Time `json:"inactive_at,omitempty"`
	ErrorCodes    []string   `json:"error_codes"`
}

type AcceleratorPolicy struct {
	Configured            bool     `json:"configured"`
	EffectiveSource       string   `json:"effective_source"`
	ScopeKind             string   `json:"scope_kind"`
	ScopeID               string   `json:"scope_id"`
	Enabled               bool     `json:"enabled"`
	SampleIntervalSeconds int      `json:"sample_interval_seconds"`
	MaximumDevices        int      `json:"maximum_devices"`
	Metrics               []string `json:"metrics"`
	Revision              int64    `json:"revision"`
}

type AcceleratorPolicyWrite struct {
	Enabled               bool     `json:"enabled"`
	SampleIntervalSeconds int      `json:"sample_interval_seconds"`
	MaximumDevices        int      `json:"maximum_devices"`
	Metrics               []string `json:"metrics"`
	ExpectedRevision      int64    `json:"expected_revision"`
}

type NodeAcceleratorsAdmin struct {
	PublicVisible    bool                     `json:"public_visible"`
	State            string                   `json:"state"`
	ErrorCode        string                   `json:"error_code,omitempty"`
	CollectorVersion string                   `json:"collector_version"`
	ObservedAt       *time.Time               `json:"observed_at,omitempty"`
	Policy           AcceleratorPolicy        `json:"policy"`
	Devices          []AcceleratorDeviceAdmin `json:"devices"`
}

type AcceleratorDeviceSelection struct {
	NodeID, DeviceID string
	PublicOnly       bool
}
