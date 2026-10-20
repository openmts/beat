package store

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func acceleratorReceiptHasMetrics(request AlertEvidenceRequest, snapshot model.AcceleratorInventorySnapshot) bool {
	for _, device := range snapshot.Devices {
		if device.Vendor != request.DeviceVendor || model.AcceleratorDeviceID(request.NodeID, device.Fingerprint) != request.DeviceID {
			continue
		}
		for _, key := range model.AcceleratorAlertInputs(request.Metric) {
			if !slices.Contains(device.PresentMetrics, key) {
				return false
			}
		}
		return true
	}
	return false
}

func (s *MTSStore) acceleratorAlertValues(ctx context.Context, selection acceleratorEvidenceRange) (map[int64]map[string]float64, error) {
	values := map[int64]map[string]float64{}
	for _, key := range model.AcceleratorAlertInputs(selection.request.Metric) {
		query, err := mts.NewQuery().From("beat", "", key).Select("value").Where(mts.TagEq("node", selection.request.NodeID), mts.TagEq("device", selection.request.DeviceID), mts.TagEq("vendor", selection.request.DeviceVendor)).TimeRange(selection.from.UnixNano(), selection.to.UnixNano()).Precision(mts.PrecisionNanosecond).OrderByTimeAsc().Limit(alerteval.MaximumWindowSamples + 1).Build()
		if err != nil {
			return nil, err
		}
		query.Budget = acceleratorAlertBudget()
		iterator, err := s.engine.QueryRowIterator(ctx, query)
		if err != nil {
			return nil, err
		}
		readErr := consumeAcceleratorValues(iterator, acceleratorValueRange{selection: selection, key: key, values: values})
		closeErr := iterator.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
	}
	return values, nil
}

type acceleratorValueRange struct {
	selection acceleratorEvidenceRange
	key       string
	values    map[int64]map[string]float64
}

func consumeAcceleratorValues(iterator mts.RowIterator, input acceleratorValueRange) error {
	count := 0
	for iterator.Next() {
		count++
		if count > alerteval.MaximumWindowSamples {
			return alerteval.ErrCapacity
		}
		row := iterator.Row()
		request := input.selection.request
		if row.Timestamp < input.selection.from.UnixNano() || row.Timestamp > input.selection.to.UnixNano() {
			continue
		}
		if !acceleratorValueTagsMatch(row, request) {
			return alerteval.ErrInvalidObservation
		}
		point, err := metricQueryPoint(row)
		if err != nil {
			return errors.Join(alerteval.ErrInvalidObservation, err)
		}
		if input.values[row.Timestamp] == nil {
			input.values[row.Timestamp] = map[string]float64{}
		}
		if _, duplicate := input.values[row.Timestamp][input.key]; duplicate {
			return alerteval.ErrInvalidObservation
		}
		input.values[row.Timestamp][input.key] = point.Value
	}
	return iterator.Err()
}

func bindAcceleratorAlertPoints(selection acceleratorEvidenceRange, receipts map[int64]acceleratorEvidenceReceipt, values map[int64]map[string]float64) ([]alerteval.EvidencePoint, error) {
	result := []alerteval.EvidencePoint{}
	for at, receipt := range receipts {
		if !receipt.present || receipt.cadence != selection.request.AcceleratorSnapshot.CadenceMS {
			continue
		}
		sample := values[at]
		if err := model.ValidateAcceleratorValues(sample, model.AcceleratorMetricKeys); err != nil {
			return nil, alerteval.ErrInvalidObservation
		}
		value, present, err := acceleratorAlertValue(selection.request.Metric, sample)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		result = append(result, alerteval.EvidencePoint{At: time.Unix(0, at).UTC(), Value: value, ID: receipt.id + ":" + selection.request.DeviceID})
	}
	return result, nil
}

func acceleratorValueTagsMatch(row mts.Row, request AlertEvidenceRequest) bool {
	return len(row.Tags) == 3 && row.Tags["node"] == request.NodeID && row.Tags["device"] == request.DeviceID && row.Tags["vendor"] == request.DeviceVendor
}

func acceleratorAlertValue(metric string, sample map[string]float64) (float64, bool, error) {
	keys := model.AcceleratorAlertInputs(metric)
	value, ok := sample[keys[0]]
	if !ok {
		return 0, false, nil
	}
	if len(keys) == 2 {
		denominator, ok := sample[keys[1]]
		if !ok || denominator <= 0 {
			return 0, false, nil
		}
		value = value / denominator * 100
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, alerteval.ErrInvalidObservation
	}
	return value, true, nil
}
