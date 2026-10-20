package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

var ErrCustomSampleSourceUnavailable = errors.New("custom sample provenance is unavailable")

type CustomMetricEvidence struct {
	Value    float64
	ReportAt time.Time
	Source   model.CustomSampleMetadata
}

func (s *MTSStore) QueryCustomSample(ctx context.Context, nodeID, metric string) (*CustomMetricEvidence, error) {
	if !strings.HasPrefix(metric, model.CustomMetricPrefix) {
		return nil, ErrMetricIngestion
	}
	ctx, cancel := context.WithTimeout(ctx, agentSnapshotTimeout)
	defer cancel()
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	release, err := s.lockAgentNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	point, err := s.queryLatestScalarPoint(ctx, latestScalarQuery{nodeID: nodeID, metric: metric, end: time.Now().Add(model.MaximumAgentClockSkew)})
	if err != nil || point == nil {
		return nil, err
	}
	receipt, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: nodeID, start: point.Timestamp.UnixNano(), end: point.Timestamp.UnixNano()})
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, ErrCustomSampleSourceUnavailable
	}
	sources, err := model.DecodeCustomSampleMetadata(receipt.CustomSamplesJSON, receipt.CapturedAt)
	if err != nil {
		return nil, ErrAgentReceiptInvalid
	}
	source, ok := sources[strings.TrimPrefix(metric, model.CustomMetricPrefix)]
	if !ok {
		return nil, ErrCustomSampleSourceUnavailable
	}
	return &CustomMetricEvidence{Value: point.Value, ReportAt: point.Timestamp, Source: source}, nil
}
