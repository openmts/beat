package handler

import (
	"net/http"
	"strings"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/diagnostics"
)

func diagnosticNode(r *http.Request) diagnostics.NodeProof {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return diagnostics.NodeProof{}
	}
	return diagnostics.NodeProof{Token: strings.TrimPrefix(header, "Bearer ")}
}

func diagnosticAttempt(r *http.Request) (diagnostics.AttemptRequest, error) {
	var request diagnosticprotocol.AttemptRequest
	if err := decodeDiagnostic(r, &request); err != nil {
		return diagnostics.AttemptRequest{}, diagnostics.ErrInvalid
	}
	if request.JobID == "" {
		request.JobID = r.PathValue("id")
	}
	if request.JobID != r.PathValue("id") {
		return diagnostics.AttemptRequest{}, diagnostics.ErrInvalid
	}
	return diagnostics.AttemptRequest{Proof: diagnosticNode(r), JobID: request.JobID,
		Attempt: request.Attempt, Nonce: request.Nonce}, nil
}

func (handler *DiagnosticHandler) admitAgent(w http.ResponseWriter) bool {
	select {
	case handler.agentSlots <- struct{}{}:
		return true
	default:
		diagnosticHeaders(w)
		JSONError(w, http.StatusTooManyRequests, "diagnostic request capacity is exhausted")
		return false
	}
}

func (handler *DiagnosticHandler) releaseAgent() { <-handler.agentSlots }

func (handler *DiagnosticHandler) HandleDiagnosticAdvertise(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var document diagnosticprotocol.CapabilityDocument
	if err := decodeDiagnostic(r, &document); err != nil {
		diagnosticResponse(w, nil, diagnostics.ErrInvalid)
		return
	}
	digest, err := handler.diagnostics.Advertise(r.Context(), diagnosticNode(r), document)
	diagnosticResponse(w, map[string]string{"digest": digest}, err)
}

func (handler *DiagnosticHandler) HandleDiagnosticLease(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var request diagnosticprotocol.LeaseRequest
	if err := decodeDiagnostic(r, &request); err != nil {
		diagnosticResponse(w, nil, diagnostics.ErrInvalid)
		return
	}
	lease, err := handler.diagnostics.Lease(r.Context(), diagnostics.LeaseRequest{Proof: diagnosticNode(r),
		CapabilityDigest: request.CapabilityDigest})
	diagnosticResponse(w, lease, err)
}

func (handler *DiagnosticHandler) HandleDiagnosticStart(w http.ResponseWriter, r *http.Request) {
	handler.agentAttempt(w, r, func(attempt diagnostics.AttemptRequest) (any, error) {
		return acknowledged{Acknowledged: true}, handler.diagnostics.Start(r.Context(), attempt)
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticHeartbeat(w http.ResponseWriter, r *http.Request) {
	handler.agentAttempt(w, r, func(attempt diagnostics.AttemptRequest) (any, error) {
		cancel, err := handler.diagnostics.Heartbeat(r.Context(), attempt)
		return map[string]bool{"cancel_requested": cancel}, err
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticProgress(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var request diagnosticprotocol.ProgressRequest
	if err := decodeDiagnostic(r, &request); err != nil {
		diagnosticResponse(w, nil, diagnostics.ErrInvalid)
		return
	}
	frame := diagnostics.ProgressFrame{Proof: diagnosticNode(r), ProgressRequest: request}
	acknowledged, err := handler.diagnostics.Progress(r.Context(), frame)
	diagnosticResponse(w, map[string]int64{"sequence": acknowledged}, err)
}

func (handler *DiagnosticHandler) HandleDiagnosticComplete(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var request diagnosticprotocol.CompletionRequest
	if err := decodeDiagnostic(r, &request); err != nil {
		diagnosticResponse(w, nil, diagnostics.ErrInvalid)
		return
	}
	completion := diagnostics.Completion{Proof: diagnosticNode(r), CompletionRequest: request}
	diagnosticResponse(w, acknowledged{Acknowledged: true}, handler.diagnostics.Complete(r.Context(), completion))
}

func (handler *DiagnosticHandler) agentAttempt(w http.ResponseWriter, r *http.Request, action func(diagnostics.AttemptRequest) (any, error)) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	attempt, err := diagnosticAttempt(r)
	if err != nil {
		diagnosticResponse(w, nil, err)
		return
	}
	value, err := action(attempt)
	diagnosticResponse(w, value, err)
}
