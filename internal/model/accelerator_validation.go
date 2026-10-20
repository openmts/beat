package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaximumAcceleratorSnapshotBytes = 64 << 10

const MinimumAcceleratorInterval = 5 * time.Second

const MaximumAcceleratorInterval = time.Hour

func ValidAcceleratorMetric(key string) bool { return slices.Contains(AcceleratorMetricKeys, key) }

func ValidAcceleratorVendor(vendor string) bool { return vendor == "nvidia" || vendor == "amd" }

func ValidAcceleratorState(state string) bool {
	return slices.Contains([]string{"disabled", "unsupported", "unavailable", "ok", "partial"}, state)
}

func ValidAcceleratorError(code string) bool {
	return slices.Contains([]string{"", "collector_missing", "collector_timeout", "output_limit", "collector_failed", "parse_failed", "identity_invalid", "partial_metrics", "policy_disabled", "platform_unsupported", "device_limit", "not_reported"}, code)
}

func ValidAcceleratorFingerprint(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == value
}

func AcceleratorDeviceID(nodeID, fingerprint string) string {
	digest := sha256.Sum256([]byte(nodeID + ":" + fingerprint))
	return hex.EncodeToString(digest[:16])
}

func ValidAcceleratorDeviceID(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 16 && hex.EncodeToString(raw) == value
}

func (report AcceleratorReport) Validate(reportAt time.Time) error {
	if err := validateAcceleratorReportIdentity(report); err != nil {
		return err
	}
	if err := validateAcceleratorReportTime(report, reportAt); err != nil {
		return err
	}
	capabilities, err := validateAcceleratorCapabilities(report.Capabilities)
	if err != nil {
		return err
	}
	if err = validateAcceleratorDevices(report, capabilities); err != nil {
		return err
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded) > MaximumAcceleratorSnapshotBytes {
		return ErrAgentReportInvalid
	}
	return nil
}

func validateAcceleratorCapabilities(values []AcceleratorCapability) (map[string]AcceleratorCapability, error) {
	if len(values) > 2 {
		return nil, ErrAgentReportInvalid
	}
	result := map[string]AcceleratorCapability{}
	for _, value := range values {
		if !ValidAcceleratorVendor(value.Vendor) || !ValidAcceleratorState(value.State) || !ValidAcceleratorError(value.ErrorCode) {
			return nil, ErrAgentReportInvalid
		}
		if _, ok := result[value.Vendor]; ok {
			return nil, ErrAgentReportInvalid
		}
		if !validAcceleratorMetricList(value.Metrics) {
			return nil, ErrAgentReportInvalid
		}
		result[value.Vendor] = value
	}
	return result, nil
}

func validAcceleratorMetricList(values []string) bool {
	if len(values) > len(AcceleratorMetricKeys) {
		return false
	}
	seen := map[string]bool{}
	for _, key := range values {
		if !ValidAcceleratorMetric(key) || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func validateAcceleratorDevices(report AcceleratorReport, capabilities map[string]AcceleratorCapability) error {
	if len(report.Devices) > MaximumAccelerators {
		return ErrAgentReportInvalid
	}
	if !acceleratorCollectionSucceeded(report.State) && len(report.Devices) > 0 {
		return ErrAgentReportInvalid
	}
	seen := map[string]bool{}
	for _, device := range report.Devices {
		capability, ok := capabilities[device.Vendor]
		if !ok || !acceleratorCollectionSucceeded(capability.State) || seen[device.Fingerprint] {
			return ErrAgentReportInvalid
		}
		seen[device.Fingerprint] = true
		if err := device.Validate(capability.Metrics); err != nil {
			return err
		}
	}
	return nil
}

func (device AcceleratorDeviceReport) Validate(advertised []string) error {
	if err := validateAcceleratorDeviceIdentity(device); err != nil {
		return err
	}
	for _, key := range device.Capabilities {
		if !slices.Contains(advertised, key) {
			return ErrAgentReportInvalid
		}
	}
	if err := ValidateAcceleratorValues(device.Metrics, device.Capabilities); err != nil {
		return err
	}
	return validateAcceleratorMetricErrors(device)
}

func ValidateAcceleratorValues(values map[string]float64, capabilities []string) error {
	for key, value := range values {
		if !slices.Contains(capabilities, key) || !validAcceleratorValue(key, value) {
			return ErrAgentReportInvalid
		}
	}
	used, hasUsed := values["gpu_memory_used_bytes"]
	total, hasTotal := values["gpu_memory_total_bytes"]
	if hasUsed && hasTotal && used > total {
		return ErrAgentReportInvalid
	}
	return nil
}

func validAcceleratorValue(key string, value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	switch key {
	case "gpu_utilization_percent":
		return acceleratorRange(value, 0, 100)
	case "gpu_memory_used_bytes", "gpu_memory_total_bytes":
		return acceleratorRange(value, 0, 1<<50) && math.Trunc(value) == value
	case "gpu_temperature_celsius":
		return acceleratorRange(value, -100, 250)
	case "gpu_power_watts", "gpu_power_limit_watts":
		return acceleratorRange(value, 0, 1000000)
	}
	return false
}

func DefaultAcceleratorPolicy() AcceleratorPolicy {
	return AcceleratorPolicy{EffectiveSource: "default", SampleIntervalSeconds: 30, MaximumDevices: MaximumAccelerators, Metrics: slices.Clone(AcceleratorMetricKeys)}
}

func ValidateAcceleratorPolicy(value AcceleratorPolicyWrite) error {
	if value.SampleIntervalSeconds < 5 || value.SampleIntervalSeconds > 3600 || value.MaximumDevices < 1 || value.MaximumDevices > MaximumAccelerators || value.ExpectedRevision < 0 {
		return ErrAgentReportInvalid
	}
	if !validAcceleratorMetricList(value.Metrics) {
		return ErrAgentReportInvalid
	}
	return nil
}

func validateAcceleratorReportIdentity(report AcceleratorReport) error {
	id, err := uuid.Parse(report.SampleID)
	if err != nil || id == uuid.Nil || id.String() != report.SampleID {
		return ErrAgentReportInvalid
	}
	if report.Version != AcceleratorReportVersion || report.CollectorVersion != "1" || report.PolicyRevision < 0 {
		return ErrAgentReportInvalid
	}
	if !ValidAcceleratorState(report.State) || !ValidAcceleratorError(report.ErrorCode) {
		return ErrAgentReportInvalid
	}
	return nil
}

func validateAcceleratorReportTime(report AcceleratorReport, at time.Time) error {
	if report.CadenceMS < MinimumAcceleratorInterval.Milliseconds() || report.CadenceMS > MaximumAcceleratorInterval.Milliseconds() {
		return ErrAgentReportInvalid
	}
	if report.CapturedAt.Before(time.Unix(0, 1)) || report.CapturedAt.After(time.Unix(0, math.MaxInt64)) || report.CapturedAt.After(at) || at.Sub(report.CapturedAt) > 2*MaximumAcceleratorInterval {
		return ErrAgentReportInvalid
	}
	return nil
}

func acceleratorCollectionSucceeded(state string) bool { return state == "ok" || state == "partial" }

func acceleratorRange(value, minimum, maximum float64) bool {
	return value >= minimum && value <= maximum
}

func validateAcceleratorDeviceIdentity(device AcceleratorDeviceReport) error {
	if !ValidAcceleratorVendor(device.Vendor) || !ValidAcceleratorFingerprint(device.Fingerprint) {
		return ErrAgentReportInvalid
	}
	if !validAgentText(device.Model, 128) || strings.TrimSpace(device.Model) == "" || !validAgentText(device.DriverVersion, 128) {
		return ErrAgentReportInvalid
	}
	if !validAcceleratorMetricList(device.Capabilities) || len(device.Errors) > len(AcceleratorMetricKeys) {
		return ErrAgentReportInvalid
	}

	return nil
}

func validateAcceleratorMetricErrors(device AcceleratorDeviceReport) error {
	seenErrors := map[string]bool{}
	for _, issue := range device.Errors {
		if !slices.Contains(device.Capabilities, issue.Metric) || seenErrors[issue.Metric] || !slices.Contains([]string{"not_supported", "unavailable", "invalid_value"}, issue.Code) {
			return ErrAgentReportInvalid
		}
		if _, present := device.Metrics[issue.Metric]; present {
			return ErrAgentReportInvalid
		}
		seenErrors[issue.Metric] = true
	}
	return nil
}
