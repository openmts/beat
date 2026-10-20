package store

import (
	"context"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func (s *MTSStore) acceleratorReportPoints(ctx context.Context, sample MetricReportSample) ([]mts.Point, error) {
	report := sample.Accelerators
	if report == nil {
		return nil, nil
	}
	if err := report.Validate(sample.Timestamp); err != nil {
		return nil, err
	}
	if err := s.checkAcceleratorIdentity(ctx, sample.NodeID, *report); err != nil {
		return nil, err
	}
	existing, err := s.readAcceleratorSample(ctx, sample.NodeID, report.CapturedAt.UnixNano())
	if err != nil {
		return nil, err
	}
	digest, err := report.Digest()
	if err != nil {
		return nil, err
	}
	if acceleratorSampleConflict(existing, *report, digest) {
		return nil, ErrAgentSampleConflict
	}
	points := acceleratorValuePoints(sample)
	receipt, err := acceleratorSamplePoint(sample.NodeID, *report)
	if err != nil {
		return nil, err
	}
	return append(points, receipt), nil
}

func (s *MTSStore) AcceleratorSampleAt(ctx context.Context, nodeID string, at int64) (*model.AcceleratorSampleReceipt, error) {
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
	receipt, err := s.readAcceleratorSample(ctx, nodeID, at)
	if err != nil || receipt == nil {
		return receipt, err
	}
	if receipt.Snapshot.CapturedAt.UnixNano() != at {
		return nil, nil
	}
	return receipt, nil
}

func acceleratorValuePoints(sample MetricReportSample) []mts.Point {
	report := sample.Accelerators
	points := make([]mts.Point, 0, len(report.Devices)*len(model.AcceleratorMetricKeys)+1)
	for _, device := range report.Devices {
		for key, value := range device.Metrics {
			if !sample.allows(key) {
				continue
			}
			points = append(points, mts.Point{Database: "beat", Measurement: key, Tags: map[string]string{"node": sample.NodeID, "device": model.AcceleratorDeviceID(sample.NodeID, device.Fingerprint), "vendor": device.Vendor}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(value)}, Timestamp: report.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond})
		}
	}

	return points
}

func acceleratorSampleConflict(existing *model.AcceleratorSampleReceipt, report model.AcceleratorReport, digest string) bool {
	return existing != nil && existing.Snapshot.CapturedAt.Equal(report.CapturedAt) && (existing.Snapshot.SampleID != report.SampleID || existing.PayloadDigest != digest)
}
