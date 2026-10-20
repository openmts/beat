package store

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func AcceleratorSampleFields() map[string]mts.FieldType {
	return map[string]mts.FieldType{"sample_id": mts.FieldString, "payload_digest": mts.FieldString, "cadence_ms": mts.FieldInt64, "inventory": mts.FieldString}
}

func acceleratorSampleFieldNames() []string {
	return []string{"sample_id", "payload_digest", "cadence_ms", "inventory"}
}

func DecodeAcceleratorSample(row mts.Row) (model.AcceleratorSampleReceipt, error) {
	var result model.AcceleratorSampleReceipt
	if err := validateAcceleratorSampleShape(row); err != nil {
		return result, err
	}
	snapshot, err := model.DecodeAcceleratorInventory(row.Fields["inventory"].String)
	if err != nil {
		return result, ErrAgentReceiptInvalid
	}
	digest := row.Fields["payload_digest"].String
	if !validReceiptDigest(digest) || snapshot.CapturedAt.UnixNano() != row.Timestamp || snapshot.SampleID != row.Fields["sample_id"].String || snapshot.CadenceMS != row.Fields["cadence_ms"].Int64 {
		return result, ErrAgentReceiptInvalid
	}
	return model.AcceleratorSampleReceipt{NodeID: row.Tags["node"], Snapshot: snapshot, PayloadDigest: digest}, nil
}

func acceleratorSamplePoint(nodeID string, report model.AcceleratorReport) (mts.Point, error) {
	inventory, err := json.Marshal(report.Inventory())
	if err != nil {
		return mts.Point{}, err
	}
	digest, err := report.Digest()
	if err != nil {
		return mts.Point{}, err
	}
	return mts.Point{Database: "beat", Measurement: model.AcceleratorSampleMeasurement, Tags: map[string]string{"node": nodeID}, Timestamp: report.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond,
		Fields: map[string]mts.FieldValue{"sample_id": mts.StringValue(report.SampleID), "payload_digest": mts.StringValue(digest), "cadence_ms": mts.Int64Value(report.CadenceMS), "inventory": mts.StringValue(string(inventory))}}, nil
}

func (s *MTSStore) LatestAcceleratorSample(ctx context.Context, nodeID string) (*model.AcceleratorSampleReceipt, error) {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = s.ensureOpen(); err != nil {
		return nil, err
	}
	release, err := s.lockAgentNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	defer release()
	if err = s.ensureOpen(); err != nil {
		return nil, err
	}
	return s.readAcceleratorSample(ctx, nodeID, math.MaxInt64)
}

func (s *MTSStore) readAcceleratorSample(ctx context.Context, nodeID string, through int64) (*model.AcceleratorSampleReceipt, error) {
	plan := latestScanPlan(time.Unix(0, 0), latestBound(through), latestProbeEnd(through))
	rows, err := scanLatestRows(ctx, plan, func(ctx context.Context, from, to time.Time) ([]mts.Row, error) {
		return s.queryAcceleratorSampleRows(ctx, nodeID, from, to)
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	sample, err := DecodeAcceleratorSample(rows[0])
	if err != nil {
		return nil, err
	}
	return &sample, nil
}

func validateAcceleratorSampleShape(row mts.Row) error {
	if len(row.Tags) != 1 || row.Tags["node"] == "" || len(row.Tags["node"]) > 128 || row.Timestamp <= 0 || len(row.Fields) != 4 {
		return ErrAgentReceiptInvalid
	}
	for name, kind := range AcceleratorSampleFields() {
		field, ok := row.Fields[name]
		if !ok || field.Type != kind {
			return ErrAgentReceiptInvalid
		}
	}

	return nil
}

// queryAcceleratorSampleRows 在给定范围内按时间倒序取一条加速器样本。
func (s *MTSStore) queryAcceleratorSampleRows(ctx context.Context, nodeID string, from, to time.Time) ([]mts.Row, error) {
	query, err := mts.NewQuery().From("beat", "", model.AcceleratorSampleMeasurement).Select(acceleratorSampleFieldNames()...).
		TimeRange(from.UnixNano(), to.UnixNano()).Precision(mts.PrecisionNanosecond).Where(mts.TagEq("node", nodeID)).OrderByTimeDesc().Limit(1).Build()
	if err != nil {
		return nil, err
	}
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: maximumAgentReceiptScan}
	return s.engine.QueryRows(ctx, query)
}
