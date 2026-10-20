package store

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

var ErrAgentReceiptInvalid = errors.New("invalid stored agent receipt")

func AgentReceiptFields() map[string]mts.FieldType {
	return map[string]mts.FieldType{"sample_id": mts.FieldString, "payload_digest": mts.FieldString,
		"credential_fingerprint": mts.FieldString, "protocol_version": mts.FieldInt64, "report_interval_ms": mts.FieldInt64, "custom_samples": mts.FieldString}
}

func agentReceiptFieldNames() []string {
	return []string{"sample_id", "payload_digest", "credential_fingerprint", "protocol_version", "report_interval_ms", "custom_samples"}
}

func DecodeAgentReceipt(row mts.Row) (model.AgentReceipt, error) {
	var receipt model.AgentReceipt
	if len(row.Tags) != 1 || row.Tags["node"] == "" || row.Timestamp <= 0 || len(row.Fields) != len(AgentReceiptFields()) {
		return receipt, ErrAgentReceiptInvalid
	}
	for field, kind := range AgentReceiptFields() {
		if value, ok := row.Fields[field]; !ok || value.Type != kind {
			return receipt, ErrAgentReceiptInvalid
		}
	}
	receipt = model.AgentReceipt{SampleID: row.Fields["sample_id"].String, PayloadDigest: row.Fields["payload_digest"].String,
		CredentialFingerprint: row.Fields["credential_fingerprint"].String, ProtocolVersion: int(row.Fields["protocol_version"].Int64),
		ReportIntervalMS: row.Fields["report_interval_ms"].Int64, CapturedAt: time.Unix(0, row.Timestamp).UTC(), CustomSamplesJSON: row.Fields["custom_samples"].String}
	id, err := uuid.Parse(receipt.SampleID)
	if err != nil || id == uuid.Nil || id.String() != receipt.SampleID || !validReceiptDigest(receipt.PayloadDigest) || !validReceiptDigest(receipt.CredentialFingerprint) {
		return model.AgentReceipt{}, ErrAgentReceiptInvalid
	}
	if receipt.ProtocolVersion != model.AgentReportProtocol || receipt.ReportIntervalMS < model.MinimumAgentReportInterval.Milliseconds() || receipt.ReportIntervalMS > model.MaximumAgentReportInterval.Milliseconds() {
		return model.AgentReceipt{}, ErrAgentReceiptInvalid
	}
	if _, err := model.DecodeCustomSampleMetadata(receipt.CustomSamplesJSON, receipt.CapturedAt); err != nil {
		return model.AgentReceipt{}, ErrAgentReceiptInvalid
	}
	return receipt, nil
}

func validReceiptDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func agentReceiptPoint(nodeID string, receipt model.AgentReceipt) mts.Point {
	if receipt.CustomSamplesJSON == "" {
		receipt.CustomSamplesJSON = "{}"
	}
	return mts.Point{Database: "beat", Measurement: model.AgentReceiptMeasurement, Tags: map[string]string{"node": nodeID},
		Timestamp: receipt.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond,
		Fields: map[string]mts.FieldValue{"sample_id": mts.StringValue(receipt.SampleID), "payload_digest": mts.StringValue(receipt.PayloadDigest),
			"credential_fingerprint": mts.StringValue(receipt.CredentialFingerprint), "protocol_version": mts.Int64Value(int64(receipt.ProtocolVersion)),
			"report_interval_ms": mts.Int64Value(receipt.ReportIntervalMS), "custom_samples": mts.StringValue(receipt.CustomSamplesJSON)}}
}

func (s *MTSStore) LatestAgentReceipt(ctx context.Context, nodeID string) (*model.AgentReceipt, error) {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: nodeID, end: math.MaxInt64})
}

type agentReceiptQuery struct {
	nodeID, sampleID string
	start, end       int64
}

func (s *MTSStore) readAgentReceipt(ctx context.Context, selection agentReceiptQuery) (*model.AgentReceipt, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	start, end := latestBound(selection.start), latestBound(selection.end)
	plan := latestScanPlan(start, end, latestProbeEnd(selection.end))
	rows, err := scanLatestRows(ctx, plan, func(ctx context.Context, from, to time.Time) ([]mts.Row, error) {
		return s.queryAgentReceiptRows(ctx, selection, from, to)
	})
	if err != nil {
		return nil, err
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	receipt, err := DecodeAgentReceipt(rows[0])
	if err != nil {
		return nil, err
	}
	return &receipt, nil
}

// queryAgentReceiptRows 在给定范围内按时间倒序取一条回报回执。
func (s *MTSStore) queryAgentReceiptRows(ctx context.Context, selection agentReceiptQuery, from, to time.Time) ([]mts.Row, error) {
	builder := mts.NewQuery().From("beat", "", model.AgentReceiptMeasurement).Select(agentReceiptFieldNames()...).
		TimeRange(from.UnixNano(), to.UnixNano()).Precision(mts.PrecisionNanosecond).Where(mts.TagEq("node", selection.nodeID)).OrderByTimeDesc().Limit(1)
	if selection.sampleID != "" {
		builder.Where(mts.FieldEq("sample_id", mts.StringValue(selection.sampleID)))
	}
	query, err := builder.Build()
	if err != nil {
		return nil, err
	}
	query.Budget.MaxSamples = maximumAgentReceiptScan
	query.Budget.MaxShards = metricQueryMaximumShards
	query.Budget.MaxParts = metricQueryMaximumParts
	return s.engine.QueryRows(ctx, query)
}

const maximumAgentReceiptScan = 1_000_000
