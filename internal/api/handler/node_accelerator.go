package handler

import (
	"context"

	"github.com/beat/backend/internal/model"
)

func (h *NodeHandler) WithAccelerators(service AcceleratorService) *NodeHandler {
	h.accelerators = service
	return h
}

func (h *MetricsHandler) WithAccelerators(service AcceleratorService) *MetricsHandler {
	h.accelerators = service
	return h
}

func (h *NodeHandler) acceleratorReportPolicy(ctx context.Context, identity model.AgentIdentity, report model.AgentReport) (*model.AcceleratorPolicy, error) {
	if h.accelerators == nil || identity.Mode != model.AgentCredentialActive {
		return nil, nil
	}
	installation := report.Accelerators != nil && report.Accelerators.InstallationEnabled
	policy, err := h.accelerators.AgentPolicy(ctx, identity.NodeID, installation)
	if err != nil {
		return nil, err
	}
	return &policy, nil
}
