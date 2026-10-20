package acceleratorcollect

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/beat/backend/internal/model"
)

var pciAddress = regexp.MustCompile(`^(?:[0-9a-fA-F]{4}|[0-9a-fA-F]{8}):[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`)

func fingerprint(vendor, pci, name string) (string, error) {
	pci = strings.ToLower(strings.TrimSpace(pci))
	if !pciAddress.MatchString(pci) || strings.TrimSpace(name) == "" || len(name) > 128 {
		return "", ErrInvalidOutput
	}
	if len(pci) == 16 && strings.HasPrefix(pci, "0000") {
		pci = pci[4:]
	}
	digest := sha256.Sum256([]byte(vendor + ":" + pci + ":" + strings.ToLower(strings.Join(strings.Fields(name), " "))))
	return hex.EncodeToString(digest[:]), nil
}

type deviceIdentity struct{ vendor, pci, name, driver string }

func device(input deviceIdentity) (model.AcceleratorDeviceReport, error) {
	vendor, pci, name, driver := input.vendor, input.pci, input.name, input.driver
	id, err := fingerprint(vendor, pci, name)
	if err != nil {
		return model.AcceleratorDeviceReport{}, err
	}
	return model.AcceleratorDeviceReport{Fingerprint: id, Vendor: vendor, Model: strings.Join(strings.Fields(name), " "), DriverVersion: strings.TrimSpace(driver), Capabilities: slices.Clone(model.AcceleratorMetricKeys), Metrics: map[string]float64{}}, nil
}

type metricField struct {
	key, raw string
	scale    float64
}

func parseValue(device *model.AcceleratorDeviceReport, input metricField) error {
	key, raw, scale := input.key, input.raw, input.scale
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(strings.ToLower(raw), "not supported") || raw == "N/A" || raw == "N/A%" {
		device.Errors = append(device.Errors, model.AcceleratorMetricError{Metric: key, Code: "unavailable"})
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return invalidMetric(device, key)
	}
	value *= scale
	if err = model.ValidateAcceleratorValues(map[string]float64{key: value}, device.Capabilities); err != nil {
		return invalidMetric(device, key)
	}
	device.Metrics[key] = value
	return nil
}

func validateDevices(devices []model.AcceleratorDeviceReport) error {
	if len(devices) > model.MaximumAccelerators {
		return ErrInvalidOutput
	}
	seen := map[string]bool{}
	for _, device := range devices {
		if seen[device.Fingerprint] {
			return ErrInvalidOutput
		}
		seen[device.Fingerprint] = true
		if err := device.Validate(model.AcceleratorMetricKeys); err != nil {
			return ErrInvalidOutput
		}
	}
	return nil
}

func invalidMetric(device *model.AcceleratorDeviceReport, key string) error {
	if key != "gpu_power_watts" && key != "gpu_power_limit_watts" {
		return ErrInvalidOutput
	}
	device.Errors = append(device.Errors, model.AcceleratorMetricError{Metric: key, Code: "invalid_value"})
	return nil
}
