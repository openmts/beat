package store

import (
	"context"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

type AcceleratorValueQuery struct {
	NodeID   string
	Snapshot model.AcceleratorInventorySnapshot
	Allowed  map[string]bool
}

func (s *MTSStore) AcceleratorValues(ctx context.Context, input AcceleratorValueQuery) (map[string]map[string]float64, error) {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = s.ensureOpen(); err != nil {
		return nil, err
	}
	release, err := s.lockAgentNode(ctx, input.NodeID)
	if err != nil {
		return nil, err
	}
	defer release()
	result := map[string]map[string]float64{}
	for _, device := range input.Snapshot.Devices {
		id := model.AcceleratorDeviceID(input.NodeID, device.Fingerprint)
		values, err := s.acceleratorDeviceValues(ctx, acceleratorValueSelection{nodeID: input.NodeID, deviceID: id, vendor: device.Vendor, at: input.Snapshot.CapturedAt, keys: device.PresentMetrics, allowed: input.Allowed})
		if err != nil {
			return nil, err
		}
		result[id] = values
	}
	if err = s.ensureOpen(); err != nil {
		return nil, err
	}
	return result, nil
}

type acceleratorValueSelection struct {
	nodeID, deviceID, vendor string
	at                       time.Time
	keys                     []string
	allowed                  map[string]bool
}

func (s *MTSStore) acceleratorDeviceValues(ctx context.Context, input acceleratorValueSelection) (map[string]float64, error) {
	values := map[string]float64{}
	for _, key := range input.keys {
		if input.allowed != nil && !input.allowed[key] {
			continue
		}
		value, found, err := s.acceleratorScalar(ctx, key, input)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		values[key] = value
	}
	if err := model.ValidateAcceleratorValues(values, model.AcceleratorMetricKeys); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *MTSStore) acceleratorScalar(ctx context.Context, key string, input acceleratorValueSelection) (float64, bool, error) {
	query, err := mts.NewQuery().From("beat", "", key).Select("value").Where(mts.TagEq("node", input.nodeID), mts.TagEq("device", input.deviceID), mts.TagEq("vendor", input.vendor)).TimeRange(input.at.UnixNano(), input.at.UnixNano()).Precision(mts.PrecisionNanosecond).OrderByTimeAsc().Limit(1).Build()
	if err != nil {
		return 0, false, err
	}
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: 1}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return 0, false, err
	}
	if len(rows) == 0 || rows[0].Timestamp != input.at.UnixNano() {
		return 0, false, nil
	}
	field, ok := rows[0].Fields["value"]
	if !ok || field.Type != mts.FieldFloat64 {
		return 0, false, ErrAgentReceiptInvalid
	}
	return field.Float64, true, nil
}
