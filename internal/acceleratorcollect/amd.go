package acceleratorcollect

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/beat/backend/internal/model"
)

func ParseAMD(raw []byte) ([]model.AcceleratorDeviceReport, error) {
	if len(raw) > MaximumOutputBytes {
		return nil, ErrOutputLimit
	}
	cards, err := boundedJSONObject(raw, model.MaximumAccelerators+1)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cards))
	for name := range cards {
		names = append(names, name)
	}
	sort.Strings(names)
	driver, err := amdSystemDriver(cards)
	if err != nil {
		return nil, err
	}
	devices := []model.AcceleratorDeviceReport{}
	for _, name := range names {
		if name == "system" {
			continue
		}
		if !strings.HasPrefix(name, "card") {
			return nil, ErrInvalidOutput
		}
		current, err := amdCard(cards[name], driver)
		if err != nil {
			return nil, err
		}
		devices = append(devices, current)
	}
	return devices, validateDevices(devices)
}

func amdDevice(fields map[string]json.RawMessage) (model.AcceleratorDeviceReport, error) {
	value, err := device(deviceIdentity{vendor: "amd", pci: amdText(fields, "PCI Bus", "PCI Bus ID"), name: amdText(fields, "Card series", "Card model"), driver: amdText(fields, "Driver version")})
	if err != nil {
		return value, err
	}
	values := []metricField{
		{key: "gpu_utilization_percent", raw: amdText(fields, "GPU use (%)"), scale: 1},
		{key: "gpu_memory_used_bytes", raw: amdText(fields, "VRAM Total Used Memory (B)"), scale: 1},
		{key: "gpu_memory_total_bytes", raw: amdText(fields, "VRAM Total Memory (B)"), scale: 1},
		{key: "gpu_temperature_celsius", raw: amdText(fields, "Temperature (Sensor edge) (C)"), scale: 1},
		{key: "gpu_power_watts", raw: amdText(fields, "Average Graphics Package Power (W)", "Current Socket Graphics Package Power (W)"), scale: 1},
		{key: "gpu_power_limit_watts", raw: amdText(fields, "Max Graphics Package Power (W)"), scale: 1},
	}
	for _, field := range values {
		if err = parseValue(&value, field); err != nil {
			return value, err
		}
	}
	return value, nil
}

func amdText(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
		var number json.Number
		if err := json.Unmarshal(raw, &number); err == nil {
			return number.String()
		}
		return "invalid_value"
	}
	return ""
}

func amdSystemDriver(cards map[string]json.RawMessage) (string, error) {
	raw, exists := cards["system"]
	if !exists {
		return "", nil
	}
	fields, err := boundedJSONObject(raw, 64)
	if err != nil {
		return "", err
	}
	return amdText(fields, "Driver version"), nil
}

func amdCard(raw json.RawMessage, driver string) (model.AcceleratorDeviceReport, error) {
	fields, err := boundedJSONObject(raw, 64)
	if err != nil {
		return model.AcceleratorDeviceReport{}, err
	}
	current, err := amdDevice(fields)
	if err != nil {
		return current, err
	}
	if current.DriverVersion == "" {
		current.DriverVersion = driver
	}
	return current, nil
}
