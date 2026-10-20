package service

import (
	"context"
	"sync"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const evaluationMaximumPreviewNodes = 8

const evaluationMaximumPreviews = 8

const evaluationMaximumClientPreviews = 2

type evaluationPreviewAdmission struct {
	mu      sync.Mutex
	active  int
	clients map[string]int
}

func (a *evaluationPreviewAdmission) acquire(client string) (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if client == "" || len(client) > 256 {
		return nil, alerteval.ErrInvalidPolicy
	}
	if a.active >= evaluationMaximumPreviews || a.clients[client] >= evaluationMaximumClientPreviews {
		return nil, alerteval.ErrCapacity
	}
	if a.clients == nil {
		a.clients = make(map[string]int)
	}
	a.active++
	a.clients[client]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.active--
			a.clients[client]--
			if a.clients[client] == 0 {
				delete(a.clients, client)
			}
		})
	}, nil
}

func (e *AlertEvaluator) Preview(ctx context.Context, client string, request model.AlertEvaluationPreviewRequest) ([]model.AlertEvaluationPreview, error) {
	release, err := e.previews.acquire(client)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, alerteval.QueryTimeout)
	defer cancel()
	if len(request.NodeIDs) < 1 || len(request.NodeIDs) > evaluationMaximumPreviewNodes {
		return nil, alerteval.ErrInvalidPolicy
	}
	if err := e.rules.Validate(ctx, request.Rule); err != nil {
		return nil, err
	}
	request.Rule.ID, request.Rule.Revision = "preview", 1
	result := make([]model.AlertEvaluationPreview, 0, len(request.NodeIDs))
	seen := make(map[string]bool, len(request.NodeIDs))
	for _, id := range request.NodeIDs {
		if seen[id] {
			return nil, alerteval.ErrInvalidPolicy
		}
		seen[id] = true
		if model.IsAcceleratorAlertMetric(request.Rule.Metric) {
			devices, err := e.states.AcceleratorTargets(ctx, id, request.Rule.Scope.DeviceID)
			if err != nil {
				return nil, err
			}
			for _, device := range devices {
				preview, err := e.previewTarget(ctx, request.Rule, store.AlertEvaluationKey{NodeID: id, DeviceID: device})
				if err != nil {
					return nil, err
				}
				result = append(result, preview)
			}
			continue
		}
		preview, err := e.previewNode(ctx, request.Rule, id)
		if err != nil {
			return nil, err
		}
		result = append(result, preview)
	}
	return result, nil
}

func (e *AlertEvaluator) previewNode(ctx context.Context, rule model.AlertEvaluationRule, nodeID string) (model.AlertEvaluationPreview, error) {
	return e.previewTarget(ctx, rule, store.AlertEvaluationKey{NodeID: nodeID})
}

func (e *AlertEvaluator) previewTarget(ctx context.Context, rule model.AlertEvaluationRule, key store.AlertEvaluationKey) (model.AlertEvaluationPreview, error) {
	nodeID := key.NodeID
	result := model.AlertEvaluationPreview{NodeID: nodeID, DeviceID: key.DeviceID}
	node, err := e.nodes.GetNode(ctx, nodeID)
	if err != nil {
		return result, err
	}
	if node == nil {
		return result, alerteval.ErrInvalidPolicy
	}
	if !evaluationNodeInScope(rule, *node) {
		result.Status, result.Reason = model.EvaluationOutOfScope, "scope_changed"
		return result, nil
	}
	input := alerteval.StepInput{Rule: rule, Node: *node, DeviceID: key.DeviceID, Observation: model.AlertEvaluationObservation{ObservedAt: e.now(), RunID: e.runID}}
	if _, err := e.observe(ctx, &input); err != nil {
		return result, err
	}
	result.DeviceName = input.DeviceName
	result.Status, result.Reason, result.Value = input.Observation.Status, input.Observation.Reason, input.Observation.Value
	result.EvidenceID = input.Observation.EvidenceID
	if !input.Observation.SourceAt.IsZero() {
		at := input.Observation.SourceAt
		result.SourceAt = &at
	}
	return result, nil
}
