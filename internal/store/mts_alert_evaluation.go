package store

import (
	"context"
	"math"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type AlertEvidenceRequest struct {
	NodeID, Metric, DefinitionVersion         string
	DeviceID, DeviceVendor, AcceleratorDigest string
	AcceleratorSnapshot                       *model.AcceleratorInventorySnapshot
	WindowSeconds                             int
	CadenceMS                                 int64
	PolicyRevision                            int64
}

type AlertEvidenceSnapshot struct {
	NodeID         string
	DeviceID       string
	Accelerator    *model.AcceleratorSampleReceipt
	PolicyRevision int64
	Receipt        *model.AgentReceipt
	Points         []alerteval.EvidencePoint
	Cadence        time.Duration
}

// ReadAlertEvidence 同时持有节点与存储读保护，避免receipt与scalar来自不同提交。
func (s *MTSStore) ReadAlertEvidence(ctx context.Context, request AlertEvidenceRequest) (AlertEvidenceSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, alerteval.QueryTimeout)
	defer cancel()
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return AlertEvidenceSnapshot{}, err
	}
	defer unlock()
	release, err := s.lockAgentNode(ctx, request.NodeID)
	if err != nil {
		return AlertEvidenceSnapshot{}, err
	}
	defer release()
	if model.IsAcceleratorAlertMetric(request.Metric) {
		return s.readAcceleratorAlertLocked(ctx, request)
	}
	receipt, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: request.NodeID, end: math.MaxInt64})
	if err != nil || receipt == nil {
		return AlertEvidenceSnapshot{}, err
	}
	return s.readAlertEvidenceLocked(ctx, request, receipt)
}

func (s *MTSStore) readAlertEvidenceLocked(ctx context.Context, request AlertEvidenceRequest, receipt *model.AgentReceipt) (AlertEvidenceSnapshot, error) {
	snapshot := AlertEvidenceSnapshot{PolicyRevision: request.PolicyRevision, NodeID: request.NodeID, Receipt: receipt, Cadence: time.Duration(receipt.ReportIntervalMS) * time.Millisecond}
	if request.CadenceMS > 0 {
		snapshot.Cadence = max(snapshot.Cadence, time.Duration(request.CadenceMS)*time.Millisecond)
	}
	if request.WindowSeconds < 0 || request.WindowSeconds > alerteval.MaximumWindowSeconds {
		return snapshot, alerteval.ErrInvalidPolicy
	}
	// 起始边界前保留一个最大连续间隔，并容纳custom上传延迟。
	lookback := time.Duration(request.WindowSeconds)*time.Second + max(2*snapshot.Cadence, 30*time.Second) + model.MaximumCustomSampleAge
	start := receipt.CapturedAt.Add(-lookback)
	points, err := s.queryRawMetric(ctx, rawMetricSelection{metric: request.Metric, nodeID: request.NodeID, start: start, end: receipt.CapturedAt.Add(time.Nanosecond), limit: alerteval.MaximumWindowSamples})
	if err != nil {
		return snapshot, err
	}
	receipts, err := s.alertReceiptRange(ctx, rawMetricSelection{nodeID: request.NodeID, start: start, end: receipt.CapturedAt.Add(time.Nanosecond)})
	if err != nil {
		return snapshot, err
	}
	snapshot.Points, err = bindAlertEvidence(points, receipts, request)
	if err != nil {
		return snapshot, err
	}
	if err := s.ensureOpen(); err != nil {
		return snapshot, err
	}
	return snapshot, ctx.Err()
}
