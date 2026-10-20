package store

import (
	"context"
	"strings"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func (s *MTSStore) alertReceiptRange(ctx context.Context, selection rawMetricSelection) (map[int64]model.AgentReceipt, error) {
	query, err := mts.NewQuery().From("beat", "", model.AgentReceiptMeasurement).Select(agentReceiptFieldNames()...).
		TimeRange(selection.start.UnixNano(), selection.end.UnixNano()).Precision(mts.PrecisionNanosecond).Where(mts.TagEq("node", selection.nodeID)).OrderByTimeAsc().Limit(alerteval.MaximumWindowSamples + 1).Build()
	if err != nil {
		return nil, err
	}
	query.Budget = mts.QueryBudget{MaxShards: metricQueryMaximumShards, MaxParts: metricQueryMaximumParts, MaxSamples: alerteval.MaximumWindowSamples}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(rows) > alerteval.MaximumWindowSamples {
		return nil, alerteval.ErrCapacity
	}
	receipts := make(map[int64]model.AgentReceipt, len(rows))
	for _, row := range rows {
		receipt, err := DecodeAgentReceipt(row)
		if err != nil {
			return nil, err
		}
		receipts[receipt.CapturedAt.UnixNano()] = receipt
	}
	return receipts, nil
}

func bindAlertEvidence(points []TimePoint, receipts map[int64]model.AgentReceipt, request AlertEvidenceRequest) ([]alerteval.EvidencePoint, error) {
	result := make([]alerteval.EvidencePoint, 0, len(points))
	for _, point := range points {
		receipt, ok := receipts[point.Timestamp.UnixNano()]
		if !ok {
			continue
		}
		evidence := alerteval.EvidencePoint{At: point.Timestamp, Value: point.Value, ID: receipt.SampleID}
		if strings.HasPrefix(request.Metric, model.CustomMetricPrefix) {
			metadata, err := model.DecodeCustomSampleMetadata(receipt.CustomSamplesJSON, receipt.CapturedAt)
			if err != nil {
				return nil, err
			}
			sample, ok := metadata[strings.TrimPrefix(request.Metric, model.CustomMetricPrefix)]
			if !ok || sample.DefinitionVersion != request.DefinitionVersion || sample.CadenceMS != request.CadenceMS {
				continue
			}
			evidence.At, evidence.ID = sample.CapturedAt, sample.SampleID
		}
		result = append(result, evidence)
	}
	return result, nil
}
