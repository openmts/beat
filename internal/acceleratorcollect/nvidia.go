package acceleratorcollect

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"

	"github.com/beat/backend/internal/model"
)

func ParseNVIDIA(raw []byte) ([]model.AcceleratorDeviceReport, error) {
	if len(raw) > MaximumOutputBytes {
		return nil, ErrOutputLimit
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = 9
	reader.TrimLeadingSpace = true
	devices := []model.AcceleratorDeviceReport{}
	for {
		fields, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrInvalidOutput
		}
		if len(devices) >= model.MaximumAccelerators {
			return nil, ErrInvalidOutput
		}
		current, err := nvidiaDevice(fields)
		if err != nil {
			return nil, err
		}
		devices = append(devices, current)
	}
	return devices, validateDevices(devices)
}

func nvidiaDevice(fields []string) (model.AcceleratorDeviceReport, error) {
	value, err := device(deviceIdentity{vendor: "nvidia", pci: fields[0], name: fields[1], driver: fields[2]})
	if err != nil {
		return value, err
	}
	const mib = 1024 * 1024
	for index, key := range model.AcceleratorMetricKeys {
		scale := float64(1)
		if key == "gpu_memory_used_bytes" || key == "gpu_memory_total_bytes" {
			scale = mib
		}
		if err = parseValue(&value, metricField{key: key, raw: fields[index+3], scale: scale}); err != nil {
			return value, err
		}
	}
	return value, nil
}
