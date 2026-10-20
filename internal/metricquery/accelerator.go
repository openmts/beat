package metricquery

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
)

type deviceReader interface {
	MetricDevice(context.Context, model.AcceleratorDeviceSelection) (*model.AcceleratorDevicePublic, error)
}

func validSeriesDevice(selection SeriesRequest) bool {
	if model.ValidAcceleratorMetric(selection.Metric) {
		return model.ValidAcceleratorDeviceID(selection.DeviceID)
	}
	return selection.DeviceID == ""
}

func (service *Service) prepareDevice(ctx context.Context, selection SeriesRequest, public bool) (string, string, error) {
	if !model.ValidAcceleratorMetric(selection.Metric) {
		return "", "", nil
	}
	reader, ok := service.nodes.(deviceReader)
	if !ok {
		return "", "", ErrUnavailable
	}
	device, err := reader.MetricDevice(ctx, model.AcceleratorDeviceSelection{NodeID: selection.NodeID, DeviceID: selection.DeviceID, PublicOnly: public})
	if err != nil {
		return "", "", err
	}
	if device == nil {
		return "", "", ErrInvalid
	}
	return device.Vendor, fmt.Sprintf("GPU %d · %s", device.Ordinal, device.Model), nil
}
