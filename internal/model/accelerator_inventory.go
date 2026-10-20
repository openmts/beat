package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"sort"
	"time"
)

const AcceleratorSampleMeasurement = "accelerator_sample"

// AcceleratorInventorySnapshot 不含任何GPU数值；可安全用于SQLite待发布/重放记录。
type AcceleratorInventorySnapshot struct {
	Version             int                          `json:"version"`
	SampleID            string                       `json:"sample_id"`
	CapturedAt          time.Time                    `json:"captured_at"`
	CadenceMS           int64                        `json:"cadence_ms"`
	CollectorVersion    string                       `json:"collector_version"`
	InstallationEnabled bool                         `json:"installation_enabled"`
	PolicyRevision      int64                        `json:"policy_revision"`
	State               string                       `json:"state"`
	ErrorCode           string                       `json:"error_code,omitempty"`
	Capabilities        []AcceleratorCapability      `json:"capabilities"`
	Devices             []AcceleratorInventoryDevice `json:"devices"`
}

type AcceleratorInventoryDevice struct {
	Fingerprint    string                   `json:"fingerprint"`
	Vendor         string                   `json:"vendor"`
	Model          string                   `json:"model"`
	DriverVersion  string                   `json:"driver_version,omitempty"`
	Capabilities   []string                 `json:"capabilities"`
	PresentMetrics []string                 `json:"present_metrics"`
	Errors         []AcceleratorMetricError `json:"errors,omitempty"`
}

func (report AcceleratorReport) Inventory() AcceleratorInventorySnapshot {
	snapshot := AcceleratorInventorySnapshot{Version: report.Version, SampleID: report.SampleID, CapturedAt: report.CapturedAt.UTC(), CadenceMS: report.CadenceMS,
		CollectorVersion: report.CollectorVersion, InstallationEnabled: report.InstallationEnabled, PolicyRevision: report.PolicyRevision, State: report.State, ErrorCode: report.ErrorCode, Capabilities: slices.Clone(report.Capabilities), Devices: []AcceleratorInventoryDevice{}}
	for _, device := range report.Devices {
		keys := slices.Collect(maps.Keys(device.Metrics))
		sort.Strings(keys)
		snapshot.Devices = append(snapshot.Devices, AcceleratorInventoryDevice{Fingerprint: device.Fingerprint, Vendor: device.Vendor, Model: device.Model, DriverVersion: device.DriverVersion, Capabilities: slices.Clone(device.Capabilities), PresentMetrics: keys, Errors: slices.Clone(device.Errors)})
	}
	return snapshot
}

func (report AcceleratorReport) Digest() (string, error) {
	report.CapturedAt = report.CapturedAt.UTC()
	encoded, err := json.Marshal(report)
	if err != nil {
		return "", ErrAgentReportInvalid
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func CloneAcceleratorReport(report *AcceleratorReport) *AcceleratorReport {
	if report == nil {
		return nil
	}
	result := *report
	result.Capabilities = slices.Clone(report.Capabilities)
	for index := range result.Capabilities {
		result.Capabilities[index].Metrics = slices.Clone(report.Capabilities[index].Metrics)
	}
	result.Devices = slices.Clone(report.Devices)
	for index := range result.Devices {
		result.Devices[index].Capabilities = slices.Clone(report.Devices[index].Capabilities)
		result.Devices[index].Metrics = maps.Clone(report.Devices[index].Metrics)
		result.Devices[index].Errors = slices.Clone(report.Devices[index].Errors)
	}
	return &result
}

type AcceleratorSampleReceipt struct {
	NodeID        string
	Snapshot      AcceleratorInventorySnapshot
	PayloadDigest string
}

func (snapshot AcceleratorInventorySnapshot) Validate() error {
	report := AcceleratorReport{Version: snapshot.Version, SampleID: snapshot.SampleID, CapturedAt: snapshot.CapturedAt, CadenceMS: snapshot.CadenceMS,
		CollectorVersion: snapshot.CollectorVersion, InstallationEnabled: snapshot.InstallationEnabled, PolicyRevision: snapshot.PolicyRevision, State: snapshot.State, ErrorCode: snapshot.ErrorCode, Capabilities: snapshot.Capabilities}
	for _, device := range snapshot.Devices {
		if !validAcceleratorMetricList(device.PresentMetrics) {
			return ErrAgentReportInvalid
		}
		for _, key := range device.PresentMetrics {
			if !slices.Contains(device.Capabilities, key) {
				return ErrAgentReportInvalid
			}
		}
		for _, issue := range device.Errors {
			if slices.Contains(device.PresentMetrics, issue.Metric) {
				return ErrAgentReportInvalid
			}
		}
		report.Devices = append(report.Devices, AcceleratorDeviceReport{Fingerprint: device.Fingerprint, Vendor: device.Vendor, Model: device.Model, DriverVersion: device.DriverVersion, Capabilities: device.Capabilities, Errors: device.Errors})
	}
	return report.Validate(snapshot.CapturedAt)
}

func DecodeAcceleratorInventory(encoded string) (AcceleratorInventorySnapshot, error) {
	var snapshot AcceleratorInventorySnapshot
	if err := decodeAcceleratorJSON([]byte(encoded), &snapshot); err != nil {
		return snapshot, err
	}
	if err := snapshot.Validate(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
