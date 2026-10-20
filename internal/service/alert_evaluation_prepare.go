package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (e *AlertEvaluator) prepare(ctx context.Context, key store.AlertEvaluationKey) (alerteval.StepInput, []store.AlertMetricPolicyReference, error) {
	metadataCtx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	var input alerteval.StepInput
	state, err := e.states.Get(metadataCtx, key)
	if err != nil {
		return input, nil, err
	}
	if state == nil {
		return input, nil, alerteval.ErrConflict
	}
	rule, err := e.rules.Get(metadataCtx, key.RuleID)
	if err != nil {
		return input, nil, err
	}
	node, err := e.nodes.GetNode(metadataCtx, key.NodeID)
	if err != nil {
		return input, nil, err
	}
	input = alerteval.StepInput{DeviceID: key.DeviceID, Rule: *rule, Previous: *state, Observation: model.AlertEvaluationObservation{ObservedAt: e.now(), RunID: e.runID}}
	input.Node = model.Node{ID: key.NodeID}
	if node != nil {
		input.Node = *node
	}
	input.Incident, err = e.events.GetActiveEventForKey(metadataCtx, key)
	if err != nil {
		return input, nil, err
	}
	input.IncidentChecked = true
	if !rule.Enabled || rule.DeletedAt != nil {
		input.Observation.Status = model.EvaluationDisabled
		return input, nil, nil
	}
	if node == nil || !evaluationNodeInScope(*rule, *node) || !evaluationDeviceInScope(*rule, key.DeviceID) {
		input.Observation.Status = model.EvaluationOutOfScope
		input.Observation.Reason = "scope_changed"
		return input, nil, nil
	}
	cancel()
	references, err := e.observe(ctx, &input)
	return input, references, err
}

func evaluationNodeInScope(rule model.AlertEvaluationRule, node model.Node) bool {
	switch rule.Scope.Kind {
	case "all":
		return true
	case "nodes":
		return slices.Contains(rule.Scope.NodeIDs, node.ID)
	case "groups":
		return slices.Contains(rule.Scope.GroupIDs, node.GroupID)
	default:
		return false
	}
}

func (e *AlertEvaluator) observe(ctx context.Context, input *alerteval.StepInput) ([]store.AlertMetricPolicyReference, error) {
	ctx, cancel := context.WithTimeout(ctx, alerteval.QueryTimeout)
	defer cancel()
	if model.IsAcceleratorAlertMetric(input.Rule.Metric) {
		return e.observeAccelerator(ctx, input)
	}
	if input.Rule.Metric == model.MetricHeartbeatAgeSeconds {
		return nil, e.observeHeartbeat(ctx, input)
	}
	if input.Rule.Metric == model.MetricTrafficUsagePercent {
		return e.observeTraffic(ctx, input)
	}
	reference, err := e.metricReference(ctx, input)
	if err != nil {
		return nil, err
	}
	references := []store.AlertMetricPolicyReference{reference}
	if input.Observation.Status != "" {
		return references, nil
	}
	request := store.AlertEvidenceRequest{NodeID: input.Node.ID, Metric: input.Rule.Metric, WindowSeconds: input.Rule.WindowSeconds, DefinitionVersion: reference.DefinitionVersion, CadenceMS: reference.CadenceMS, PolicyRevision: reference.Revision}
	snapshot, err := e.metrics.ReadAlertEvidence(ctx, request)
	if err != nil {
		input.Observation.Status = model.EvaluationError
		input.Observation.Reason = "storage_unavailable"
		return references, nil
	}
	input.Observation = evaluationObservation(input.Observation, input.Rule, snapshot)
	return references, nil
}

func (e *AlertEvaluator) metricReference(ctx context.Context, input *alerteval.StepInput) (store.AlertMetricPolicyReference, error) {
	reference := store.AlertMetricPolicyReference{}
	definition, err := e.catalog.Get(ctx, input.Rule.Metric)
	if err != nil {
		return reference, err
	}
	if definition == nil {
		return reference, alerteval.ErrConflict
	}
	reference = store.AlertMetricPolicyReference{Key: definition.Key, Revision: definition.Revision}
	if !definition.Enabled {
		input.Observation.Status = model.EvaluationDisabled
		input.Observation.Reason = "metric_disabled"
		return reference, nil
	}
	if err := alerteval.ValidateRule(input.Rule, definition); err != nil {
		input.Observation.Status = model.EvaluationError
		input.Observation.Reason = "invalid_policy"
		return reference, nil
	}
	if strings.HasPrefix(input.Rule.Metric, model.CustomMetricPrefix) {
		reference, err = e.customReference(ctx, *input, reference)
		if err != nil {
			return reference, err
		}
		if reference.DefinitionVersion == "" {
			input.Observation.Status = model.EvaluationOutOfScope
			input.Observation.Reason = "custom_unbound"
		}
	}
	return reference, nil
}

func (e *AlertEvaluator) customReference(ctx context.Context, input alerteval.StepInput, reference store.AlertMetricPolicyReference) (store.AlertMetricPolicyReference, error) {
	metrics, err := e.custom.ListEnabledForNode(ctx, input.Node.ID)
	if err != nil {
		return reference, err
	}
	for _, metric := range metrics {
		if model.CustomMetricPrefix+metric.Name == input.Rule.Metric {
			reference.DefinitionVersion = model.CustomMetricDefinitionVersion(metric)
			reference.CadenceMS = int64(metric.IntervalSeconds) * time.Second.Milliseconds()
			break
		}
	}
	return reference, nil
}

func evaluationDeviceInScope(rule model.AlertEvaluationRule, deviceID string) bool {
	if !model.IsAcceleratorAlertMetric(rule.Metric) {
		return deviceID == ""
	}
	return model.ValidAcceleratorDeviceID(deviceID) && (rule.Scope.DeviceID == model.AcceleratorAnyDevice || rule.Scope.DeviceID == deviceID)
}
