package store

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

var (
	ErrAgentSampleConflict = errors.New("agent sample identity conflicts with a committed or pending sample")
	ErrAgentSampleOrder    = errors.New("agent sample is out of order")
	ErrAgentIngestCapacity = errors.New("agent ingestion capacity is exhausted")
	ErrAgentIngestClosed   = errors.New("agent ingestion attempt is closed")
)

type AgentReportInput struct {
	Identity model.AgentIdentity
	Report   model.AgentReport
	Now      time.Time
}

type AgentMetricPolicy func(context.Context) (MetricReportSample, error)

type AgentIngestAttempt struct {
	store     *MTSStore
	nodeID    string
	receipt   model.AgentReceipt
	prepared  *preparedAgentReport
	duplicate bool
	release   func()
	once      sync.Once
	closed    bool
}

func (attempt *AgentIngestAttempt) Duplicate() bool { return attempt.duplicate }

func (s *MTSStore) PrepareAgentReport(ctx context.Context, input AgentReportInput, policy AgentMetricPolicy) (*AgentIngestAttempt, error) {
	if err := input.Report.Validate(input.Now); err != nil {
		return nil, err
	}
	if input.Identity.NodeID == "" || !validReceiptDigest(input.Identity.CredentialFingerprint) {
		return nil, model.ErrAgentReportInvalid
	}
	digest, err := input.Report.Digest()
	if err != nil {
		return nil, err
	}
	sources, err := model.EncodeCustomSampleMetadata(input.Report.CustomSamples)
	if err != nil {
		return nil, err
	}
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	lock := s.nodeLock(input.Identity.NodeID)
	if !lock.TryLock() {
		unlock()
		return nil, ErrAgentIngestCapacity
	}
	attempt := &AgentIngestAttempt{store: s, nodeID: input.Identity.NodeID, release: func() { lock.Unlock(); unlock() },
		receipt: model.AgentReceipt{SampleID: input.Report.SampleID, CapturedAt: input.Report.CapturedAt.UTC(), PayloadDigest: digest,
			CredentialFingerprint: input.Identity.CredentialFingerprint, ProtocolVersion: model.AgentReportProtocol, ReportIntervalMS: input.Report.ReportIntervalMS, CustomSamplesJSON: sources}}
	if err := s.prepareAgentAttempt(ctx, attempt, agentPreparation{input: input, policy: policy}); err != nil {
		attempt.Close()
		return nil, err
	}
	return attempt, nil
}

type agentPreparation struct {
	input  AgentReportInput
	policy AgentMetricPolicy
}

func (s *MTSStore) prepareAgentAttempt(ctx context.Context, attempt *AgentIngestAttempt, preparation agentPreparation) error {
	input, policy := preparation.input, preparation.policy
	existing, err := s.findMatchingAgentReceipt(ctx, input)
	if err != nil {
		return err
	}
	if existing != nil {
		if *existing != attempt.receipt {
			return ErrAgentSampleConflict
		}
		attempt.duplicate = true
		return nil
	}
	prepared, err := s.reserveAgentPlan(attempt, input.Now)
	if err != nil {
		return err
	}
	attempt.prepared = prepared
	if prepared.points != nil {
		return nil
	}
	sample := MetricReportSample{NodeID: attempt.nodeID, Metrics: input.Report.Metrics, Timestamp: input.Report.CapturedAt}
	if policy != nil {
		sample, err = policy(ctx)
		if err != nil {
			return err
		}
	} else if len(input.Report.CustomMetrics) > 0 {
		return ErrMetricIngestion
	}
	sample.NodeID, sample.Metrics, sample.Timestamp = attempt.nodeID, input.Report.Metrics, input.Report.CapturedAt
	sample.Accelerators = input.Report.Accelerators
	if input.Identity.Mode != model.AgentCredentialActive && sample.Accelerators != nil {
		if sample.Accelerators.State != "disabled" || sample.Accelerators.InstallationEnabled {
			return ErrAgentCredentialRejected
		}
		sample.Accelerators = nil
	}
	values, err := s.metricReportValues(ctx, sample)
	if err != nil {
		return err
	}
	extra, err := s.acceleratorReportPoints(ctx, sample)
	if err != nil {
		return err
	}
	prepared.points = append(agentReportPoints(attempt.nodeID, values, attempt.receipt), extra...)
	return nil
}

func (s *MTSStore) findMatchingAgentReceipt(ctx context.Context, input AgentReportInput) (*model.AgentReceipt, error) {
	at := input.Report.CapturedAt.UnixNano()
	exact, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: input.Identity.NodeID, start: at, end: at})
	if err != nil || exact != nil {
		return exact, err
	}
	byID, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: input.Identity.NodeID, sampleID: input.Report.SampleID,
		start: input.Now.Add(-model.MaximumAgentSampleAge).UnixNano(), end: input.Now.Add(model.MaximumAgentClockSkew).UnixNano()})
	if err != nil || byID != nil {
		return byID, err
	}
	latest, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: input.Identity.NodeID, end: math.MaxInt64})
	if err != nil {
		return nil, err
	}
	if latest != nil && !input.Report.CapturedAt.After(latest.CapturedAt) {
		return nil, ErrAgentSampleOrder
	}
	return nil, nil
}

func agentReportPoints(nodeID string, values []model.MetricValue, receipt model.AgentReceipt) []mts.Point {
	points := make([]mts.Point, 0, len(values)+1)
	for _, value := range values {
		points = append(points, mts.Point{Database: "beat", Measurement: value.Name, Tags: map[string]string{"node": nodeID},
			Fields: map[string]mts.FieldValue{"value": mts.Float64Value(value.Value)}, Timestamp: receipt.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond})
	}
	return append(points, agentReceiptPoint(nodeID, receipt))
}

func (attempt *AgentIngestAttempt) Commit(ctx context.Context) (bool, error) {
	if attempt.closed {
		return false, ErrAgentIngestClosed
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := attempt.store.ensureOpen(); err != nil {
		return false, err
	}
	defer attempt.store.observeAgentCommit(time.Now())
	if attempt.duplicate {
		return true, attempt.store.engine.confirmDurability(ctx)
	}
	attempt.prepared.uncertain = true
	if err := attempt.store.engine.Write(ctx, attempt.prepared.points, mts.WriteOptions{Sync: true}); err != nil {
		return false, err
	}
	attempt.store.removeAgentPlan(attempt.nodeID, attempt.prepared)
	attempt.prepared = nil
	attempt.duplicate = true
	return false, nil
}

func (attempt *AgentIngestAttempt) Close() {
	attempt.once.Do(func() {
		if attempt.prepared != nil {
			attempt.store.releaseAgentPlan(attempt.nodeID, attempt.prepared)
		}
		attempt.closed = true
		attempt.release()
	})
}
