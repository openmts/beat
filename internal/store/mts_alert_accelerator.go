package store

import (
	"context"
	"errors"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type acceleratorEvidenceRange struct {
	request  AlertEvidenceRequest
	from, to time.Time
}

type acceleratorEvidenceReceipt struct {
	id      string
	present bool
	cadence int64
}

func (s *MTSStore) readAcceleratorAlertLocked(ctx context.Context, request AlertEvidenceRequest) (AlertEvidenceSnapshot, error) {
	result := AlertEvidenceSnapshot{NodeID: request.NodeID, DeviceID: request.DeviceID, PolicyRevision: request.PolicyRevision}
	if !validAcceleratorAlertRequest(request) {
		return result, alerteval.ErrInvalidPolicy
	}
	if request.AcceleratorSnapshot == nil {
		return result, nil
	}
	published := request.AcceleratorSnapshot
	receipt, err := s.readAcceleratorSample(ctx, request.NodeID, published.CapturedAt.UnixNano())
	if err != nil || receipt == nil {
		return result, err
	}
	if !matchesPublishedAccelerator(*receipt, request) {
		return result, alerteval.ErrInvalidObservation
	}
	result.Accelerator = receipt
	result.Cadence = time.Duration(receipt.Snapshot.CadenceMS) * time.Millisecond
	selection := acceleratorEvidenceRange{request: request, to: receipt.Snapshot.CapturedAt}
	selection.from = selection.to.Add(-time.Duration(request.WindowSeconds)*time.Second - max(2*result.Cadence, 30*time.Second))
	receipts, err := s.acceleratorAlertReceipts(ctx, selection)
	if err != nil {
		return result, err
	}
	values, err := s.acceleratorAlertValues(ctx, selection)
	if err != nil {
		return result, err
	}
	result.Points, err = bindAcceleratorAlertPoints(selection, receipts, values)
	if err != nil {
		return result, err
	}
	if err := s.ensureOpen(); err != nil {
		return result, err
	}
	return result, ctx.Err()
}

func (s *MTSStore) acceleratorAlertReceipts(ctx context.Context, selection acceleratorEvidenceRange) (map[int64]acceleratorEvidenceReceipt, error) {
	query, err := mts.NewQuery().From("beat", "", model.AcceleratorSampleMeasurement).Select(acceleratorSampleFieldNames()...).Where(mts.TagEq("node", selection.request.NodeID)).TimeRange(selection.from.UnixNano(), selection.to.UnixNano()).Precision(mts.PrecisionNanosecond).OrderByTimeAsc().Limit(alerteval.MaximumWindowSamples + 1).Build()
	if err != nil {
		return nil, err
	}
	query.Budget = acceleratorAlertBudget()
	iterator, err := s.engine.QueryRowIterator(ctx, query)
	if err != nil {
		return nil, err
	}
	receipts, readErr := consumeAcceleratorReceipts(ctx, iterator, selection)
	closeErr := iterator.Close()
	return receipts, errors.Join(readErr, closeErr)
}

func acceleratorAlertBudget() mts.QueryBudget {
	return mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: alerteval.MaximumWindowSamples}
}

func consumeAcceleratorReceipts(ctx context.Context, iterator mts.RowIterator, selection acceleratorEvidenceRange) (map[int64]acceleratorEvidenceReceipt, error) {
	receipts := map[int64]acceleratorEvidenceReceipt{}
	seen := map[string]int64{}
	count := 0
	for iterator.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count++
		if count > alerteval.MaximumWindowSamples {
			return nil, alerteval.ErrCapacity
		}
		row := iterator.Row()
		if !withinAcceleratorEvidenceRange(row.Timestamp, selection) {
			continue
		}
		receipt, err := decodeAcceleratorEvidenceReceipt(row, selection.request.NodeID)
		if err != nil {
			return nil, alerteval.ErrInvalidObservation
		}
		if old, ok := seen[receipt.Snapshot.SampleID]; ok && old != row.Timestamp {
			return nil, alerteval.ErrInvalidObservation
		}
		seen[receipt.Snapshot.SampleID] = row.Timestamp
		if receipt.Snapshot.PolicyRevision != selection.request.AcceleratorSnapshot.PolicyRevision {
			continue
		}
		present := acceleratorReceiptHasMetrics(selection.request, receipt.Snapshot)
		if _, duplicate := receipts[row.Timestamp]; duplicate {
			return nil, alerteval.ErrInvalidObservation
		}
		receipts[row.Timestamp] = acceleratorEvidenceReceipt{id: receipt.Snapshot.SampleID, present: present, cadence: receipt.Snapshot.CadenceMS}
	}
	return receipts, iterator.Err()
}

func validAcceleratorAlertRequest(request AlertEvidenceRequest) bool {
	return model.ValidAcceleratorDeviceID(request.DeviceID) && request.WindowSeconds >= 0 && request.WindowSeconds <= alerteval.MaximumWindowSeconds
}

func matchesPublishedAccelerator(receipt model.AcceleratorSampleReceipt, request AlertEvidenceRequest) bool {
	return receipt.Snapshot.SampleID == request.AcceleratorSnapshot.SampleID && receipt.Snapshot.CapturedAt.Equal(request.AcceleratorSnapshot.CapturedAt) && receipt.PayloadDigest == request.AcceleratorDigest
}

func withinAcceleratorEvidenceRange(at int64, selection acceleratorEvidenceRange) bool {
	return at >= selection.from.UnixNano() && at <= selection.to.UnixNano()
}

func decodeAcceleratorEvidenceReceipt(row mts.Row, nodeID string) (model.AcceleratorSampleReceipt, error) {
	receipt, err := DecodeAcceleratorSample(row)
	if err != nil || row.Tags["node"] != nodeID {
		return model.AcceleratorSampleReceipt{}, alerteval.ErrInvalidObservation
	}
	return receipt, nil
}
