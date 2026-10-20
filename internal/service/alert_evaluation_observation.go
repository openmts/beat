package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func evaluationObservation(observation model.AlertEvaluationObservation, rule model.AlertEvaluationRule, snapshot store.AlertEvidenceSnapshot) model.AlertEvaluationObservation {
	observation.Status, observation.Reason = model.EvaluationNoData, "missing_receipt"
	var receiptAt time.Time
	if snapshot.Accelerator != nil {
		receiptAt = snapshot.Accelerator.Snapshot.CapturedAt
	} else if snapshot.Receipt != nil {
		receiptAt = snapshot.Receipt.CapturedAt
	} else {
		return observation
	}
	if !freshEvaluationSource(observation.ObservedAt, receiptAt, snapshot) {
		observation.Reason = "stale_receipt"
		return observation
	}
	observation.Reason = "missing_value"
	if len(snapshot.Points) == 0 {
		return observation
	}
	points := slices.Clone(snapshot.Points)
	slices.SortFunc(points, func(a, b alerteval.EvidencePoint) int { return a.At.Compare(b.At) })
	last := points[len(points)-1]
	if (snapshot.Accelerator != nil || rule.WindowSeconds > 0 && !strings.HasPrefix(rule.Metric, model.CustomMetricPrefix)) && !last.At.Equal(receiptAt) {
		if rule.WindowSeconds > 0 {
			observation.Reason = "incomplete_window"
		}
		return observation
	}
	observation.Cadence = snapshot.Cadence
	if !freshEvaluationSource(observation.ObservedAt, last.At, snapshot) {
		observation.Reason = "stale_value"
		return observation
	}
	value, err := evaluationValue(rule, points, snapshot.Cadence)
	if err != nil {
		observation.Reason = "incomplete_window"
		if !errors.Is(err, alerteval.ErrIncompleteWindow) {
			observation.Status = model.EvaluationError
			observation.Reason = "invalid_evidence"
		}
		return observation
	}
	from := last.At.Add(-time.Duration(rule.WindowSeconds) * time.Second)
	identityNode := snapshot.NodeID
	if snapshot.DeviceID != "" {
		identityNode += ":" + snapshot.DeviceID
	}
	identity := fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s|%d", rule.Metric, last.ID, rule.Revision, rule.Aggregation, from.Format(time.RFC3339Nano), last.At.Format(time.RFC3339Nano), identityNode, snapshot.PolicyRevision)
	observation.EvidenceID = fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	observation.SourceAt = last.At
	observation.Status, observation.Reason = model.EvaluationOK, ""
	observation.Value = &value
	return observation
}

func evaluationValue(rule model.AlertEvaluationRule, points []alerteval.EvidencePoint, cadence time.Duration) (float64, error) {
	latest := points[len(points)-1]
	if rule.WindowSeconds > 0 {
		return alerteval.AggregateWindow(alerteval.EvidenceWindow{From: latest.At.Add(-time.Duration(rule.WindowSeconds) * time.Second), To: latest.At, Cadence: cadence, Aggregation: rule.Aggregation, Points: points})
	}
	for _, point := range points {
		if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) || point.ID == "" {
			return 0, alerteval.ErrInvalidObservation
		}
		if point.ID == latest.ID && (point.Value != latest.Value || !point.At.Equal(latest.At)) {
			return 0, alerteval.ErrInvalidObservation
		}
	}
	return latest.Value, nil
}

func freshEvaluationSource(now, captured time.Time, snapshot store.AlertEvidenceSnapshot) bool {
	if snapshot.Accelerator != nil {
		return !captured.IsZero() && !captured.After(now.Add(model.MaximumAgentClockSkew)) && now.Sub(captured) <= max(3*snapshot.Cadence, 30*time.Second)
	}
	return model.NewTelemetryFreshness(now, &captured, snapshot.Cadence).TelemetryState == model.TelemetryFresh
}
