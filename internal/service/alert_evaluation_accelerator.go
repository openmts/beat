package service

import (
	"context"
	"errors"
	"slices"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (e *AlertEvaluator) observeAccelerator(ctx context.Context, input *alerteval.StepInput) ([]store.AlertMetricPolicyReference, error) {
	source, err := e.states.AcceleratorContext(ctx, store.AlertEvaluationKey{RuleID: input.Rule.ID, NodeID: input.Node.ID, DeviceID: input.DeviceID})
	if err != nil {
		return nil, err
	}
	input.DeviceName = source.Name
	references, err := e.acceleratorReferences(ctx, input, source)
	if err != nil {
		return nil, err
	}
	if input.Observation.Status != "" {
		return references, nil
	}
	status, reason := acceleratorCollectionStatus(input, source)
	if status != "" {
		input.Observation.Status, input.Observation.Reason = status, reason
		return references, nil
	}
	request := store.AlertEvidenceRequest{NodeID: input.Node.ID, DeviceID: input.DeviceID, DeviceVendor: source.Vendor, Metric: input.Rule.Metric, WindowSeconds: input.Rule.WindowSeconds, PolicyRevision: references[0].Revision, AcceleratorSnapshot: source.Snapshot, AcceleratorDigest: source.Digest}
	snapshot, err := e.metrics.ReadAlertEvidence(ctx, request)
	if err != nil {
		input.Observation.Status, input.Observation.Reason = model.EvaluationError, "storage_unavailable"
		if errors.Is(err, alerteval.ErrInvalidObservation) || errors.Is(err, store.ErrAgentReceiptInvalid) {
			input.Observation.Reason = "invalid_evidence"
		}
		return references, nil
	}
	input.Observation = evaluationObservation(input.Observation, input.Rule, snapshot)
	return references, nil
}

func (e *AlertEvaluator) acceleratorReferences(ctx context.Context, input *alerteval.StepInput, source store.AlertAcceleratorContext) ([]store.AlertMetricPolicyReference, error) {
	var references []store.AlertMetricPolicyReference
	for _, key := range model.AcceleratorAlertInputs(input.Rule.Metric) {
		definition, err := e.catalog.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if definition == nil {
			return nil, alerteval.ErrConflict
		}
		reference := store.AlertMetricPolicyReference{Key: key, Revision: definition.Revision, AcceleratorPolicyRevision: source.Policy.Revision, AcceleratorDeviceID: input.DeviceID, AcceleratorDigest: source.Digest}
		if source.Snapshot != nil {
			reference.AcceleratorSampleID = source.Snapshot.SampleID
		}
		references = append(references, reference)
		if !definition.Enabled {
			input.Observation.Status, input.Observation.Reason = model.EvaluationDisabled, "metric_disabled"
		}
	}
	definition, err := e.catalog.Get(ctx, input.Rule.Metric)
	if err != nil {
		return nil, err
	}
	if err := alerteval.ValidateRule(input.Rule, definition); err != nil && input.Observation.Status == "" {
		input.Observation.Status, input.Observation.Reason = model.EvaluationError, "invalid_policy"
	}
	return references, nil
}

func acceleratorCollectionStatus(input *alerteval.StepInput, source store.AlertAcceleratorContext) (string, string) {
	if source.DeviceUnavailable(input.Node.ID, input.DeviceID) {
		return model.EvaluationOutOfScope, model.AcceleratorDeviceUnavailable
	}
	if !source.Found {
		return model.EvaluationNoData, "missing_device"
	}
	if !source.Policy.Enabled {
		return model.EvaluationDisabled, "collector_disabled"
	}
	for _, metric := range model.AcceleratorAlertInputs(input.Rule.Metric) {
		if !slices.Contains(source.Policy.Metrics, metric) {
			return model.EvaluationDisabled, "accelerator_metric_disabled"
		}
	}
	if source.Snapshot == nil {
		return model.EvaluationNoData, "missing_receipt"
	}
	if source.Snapshot.PolicyRevision != source.Policy.Revision {
		return model.EvaluationNoData, "accelerator_policy_changed"
	}
	return acceleratorSnapshotStatus(source)
}

func acceleratorSnapshotStatus(source store.AlertAcceleratorContext) (string, string) {
	if status, reason := acceleratorReportedState(source.Snapshot.State); status != "" {
		return status, reason
	}
	for _, capability := range source.Snapshot.Capabilities {
		if capability.Vendor == source.Vendor {
			return acceleratorReportedState(capability.State)
		}
	}
	return "", ""
}

func acceleratorReportedState(state string) (string, string) {
	switch state {
	case "disabled":
		return model.EvaluationDisabled, "collector_disabled"
	case "unsupported":
		return model.EvaluationNoData, "accelerator_unsupported"
	case "unavailable":
		return model.EvaluationError, "accelerator_unavailable"
	}
	return "", ""
}
