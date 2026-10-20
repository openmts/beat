package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (h *NodeHandler) decodeAgentReport(w http.ResponseWriter, r *http.Request, report *model.AgentReport) bool {
	r.Body = http.MaxBytesReader(w, r.Body, model.MaximumAgentReportBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(report)
	if err == nil {
		var extra any
		err = decoder.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return true
		}
	}
	var maximum *http.MaxBytesError
	status := http.StatusBadRequest
	if errors.As(err, &maximum) {
		status = http.StatusRequestEntityTooLarge
	}
	h.finishAgentACK(w, status, model.AgentReportACK{ProtocolVersion: model.AgentReportProtocol, ErrorCode: "invalid_report"})
	return false
}

func (h *NodeHandler) writeAgentReportError(w http.ResponseWriter, ack model.AgentReportACK, err error) {
	status := http.StatusServiceUnavailable
	ack.ErrorCode, ack.Retryable = "storage_unavailable", true
	switch {
	case errors.Is(err, model.ErrAgentReportProtocol):
		status, ack.ErrorCode, ack.Retryable = http.StatusUpgradeRequired, "unsupported_protocol", false
	case errors.Is(err, model.ErrAgentReportFuture):
		status, ack.ErrorCode, ack.Retryable = http.StatusUnprocessableEntity, "clock_skew", false
	case errors.Is(err, model.ErrAgentReportExpired):
		status, ack.ErrorCode, ack.Retryable = http.StatusUnprocessableEntity, "sample_expired", false
	case errors.Is(err, model.ErrAgentReportInvalid), errors.Is(err, store.ErrMetricIngestion):
		status, ack.ErrorCode, ack.Retryable = http.StatusBadRequest, "invalid_report", false
	case errors.Is(err, store.ErrAcceleratorPolicy):
		status, ack.ErrorCode, ack.Retryable = http.StatusConflict, "accelerator_policy_changed", false
	case errors.Is(err, store.ErrAcceleratorCapacity):
		status, ack.ErrorCode, ack.Retryable = http.StatusBadRequest, "accelerator_capacity", false
	case errors.Is(err, store.ErrAgentSampleConflict):
		status, ack.ErrorCode, ack.Retryable = http.StatusConflict, "sample_conflict", false
	case errors.Is(err, store.ErrCustomSourceChanged):
		status, ack.ErrorCode, ack.Retryable = http.StatusConflict, "custom_source_changed", false
	case errors.Is(err, store.ErrAgentSampleOrder):
		status, ack.ErrorCode, ack.Retryable = http.StatusConflict, "sample_out_of_order", false
	case errors.Is(err, store.ErrAgentCredentialRejected):
		status, ack.ErrorCode, ack.Retryable = http.StatusUnauthorized, "credential_rejected", false
	case errors.Is(err, store.ErrAgentIngestCapacity):
		status, ack.ErrorCode = http.StatusTooManyRequests, "ingestion_capacity"
	case errors.Is(err, store.ErrMTSRecoveryRequired):
		ack.ErrorCode = "storage_recovery_required"
	}
	if ack.Retryable {
		w.Header().Set("Retry-After", "1")
	}
	h.finishAgentACK(w, status, ack)
}
