package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const agentReportTimeout = 10 * time.Second

func (h *NodeHandler) handleMetricReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body model.AgentReport
	if !h.decodeAgentReport(w, r, &body) {
		return
	}
	ack := model.AgentReportACK{ProtocolVersion: model.AgentReportProtocol, SampleID: body.SampleID, CapturedAt: body.CapturedAt}
	policy, policyErr := h.acceleratorReportPolicy(r.Context(), identity, body)
	if policyErr != nil {
		h.writeAgentReportError(w, ack, policyErr)
		return
	}
	ack.AcceleratorPolicy = policy
	now := time.Now()
	if err := body.Validate(now); err != nil {
		h.writeAgentReportError(w, ack, err)
		return
	}
	release, ok := h.ingestBudget.acquire(r.Context(), middleware.ClientIP(r), identity.NodeID)
	if !ok {
		h.writeAgentReportError(w, ack, store.ErrAgentIngestCapacity)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), agentReportTimeout)
	defer cancel()
	if h.mtsStore == nil {
		h.writeAgentReportError(w, ack, store.ErrMTSStoreClosed)
		return
	}
	attempt, err := h.mtsStore.PrepareAgentReport(ctx, store.AgentReportInput{Identity: identity, Report: body, Now: now},
		func(ctx context.Context) (store.MetricReportSample, error) {
			return h.prepareMetricReport(ctx, identity.NodeID, body)
		})
	if err != nil {
		h.writeAgentReportError(w, ack, err)
		return
	}
	defer attempt.Close()
	h.commitAgentReport(ctx, w, agentReportCommit{contact: store.AgentContact{Identity: identity, Accelerators: body.Accelerators, ReportID: body.SampleID, ReportCapturedAt: body.CapturedAt, SourceIP: middleware.ClientIP(r), Heartbeat: store.NodeHeartbeat{Host: body.Host, Port: body.Port, System: body.System}}, attempt: attempt, ack: ack})
}

type agentReportCommit struct {
	contact store.AgentContact
	attempt *store.AgentIngestAttempt
	ack     model.AgentReportACK
}

func (h *NodeHandler) commitAgentReport(ctx context.Context, w http.ResponseWriter, commit agentReportCommit) {
	contact, attempt, ack := commit.contact, commit.attempt, commit.ack
	contact.PreserveInventory = attempt.Duplicate()
	node, err := h.nodeStore.AcceptAgentContact(ctx, contact)
	if err != nil {
		h.writeAgentReportError(w, ack, err)
		return
	}
	ack.ContactAccepted = true
	ack.ContactAt = &node.LastSeen
	duplicate, err := attempt.Commit(ctx)
	if err != nil {
		h.writeAgentReportError(w, ack, err)
		return
	}
	ack.MetricsAccepted, ack.Duplicate = true, duplicate
	attempt.Close()
	if h.accelerators != nil && contact.Accelerators != nil && contact.Identity.Mode == model.AgentCredentialActive {
		if err := h.accelerators.PublishReport(ctx, contact.Identity.NodeID, ack.SampleID); err != nil {
			h.writeAgentReportError(w, ack, err)
			return
		}
	}
	ack.SSHPublicKey = node.SSHPublicKey
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	h.finishAgentACK(w, status, ack)
}

func (h *NodeHandler) prepareMetricReport(ctx context.Context, nodeID string, body model.AgentReport) (store.MetricReportSample, error) {
	sample := store.MetricReportSample{NodeID: nodeID, Metrics: body.Metrics, Timestamp: body.CapturedAt}
	if len(body.CustomMetrics) > 0 && h.metricCatalog == nil {
		return sample, errors.New("metric catalog is required")
	}
	if h.metricCatalog != nil {
		var err error
		sample, err = h.metricCatalog.PrepareAgentPolicy(ctx, store.AgentMetricPolicyInput{NodeID: nodeID, Values: body.CustomMetrics, Samples: body.CustomSamples})
		if err != nil {
			return sample, err
		}
	}
	sample.NodeID, sample.Metrics, sample.Timestamp = nodeID, body.Metrics, body.CapturedAt
	return sample, nil
}
