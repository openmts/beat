package handler

import (
	"net/http"
	"strings"

	"github.com/beat/backend/internal/remoteoperations"
)

func operationNode(r *http.Request) remoteoperations.NodeProof {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return remoteoperations.NodeProof{}
	}
	return remoteoperations.NodeProof{Token: strings.TrimPrefix(header, "Bearer ")}
}

func operationAttempt(r *http.Request, nonce string) remoteoperations.AttemptRequest {
	return remoteoperations.AttemptRequest{Node: operationNode(r), ID: r.PathValue("id"), Nonce: nonce}
}

func (handler *RemoteOperationsHandler) HandleOperationLease(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var input remoteoperations.AgentLeaseInput
	if err := decodeOperation(r, &input); err != nil {
		operationResponse(w, nil, err)
		return
	}
	digest, err := handler.operations.Advertise(r.Context(), operationNode(r), input.Capabilities)
	if err != nil {
		operationResponse(w, nil, err)
		return
	}
	if digest != input.CapabilityDigest {
		operationResponse(w, nil, remoteoperations.ErrConflict)
		return
	}
	lease, err := handler.operations.Lease(r.Context(), remoteoperations.LeaseRequest{Proof: operationNode(r), CapabilityDigest: digest})
	operationResponse(w, lease, err)
}

func (handler *RemoteOperationsHandler) HandleOperationStart(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var input remoteoperations.AgentAttemptInput
	if err := decodeOperation(r, &input); err != nil {
		operationResponse(w, nil, err)
		return
	}
	err := handler.operations.Start(r.Context(), operationAttempt(r, input.Nonce))
	operationResponse(w, remoteoperations.StartAuthorization{Authorized: true}, err)
}

func (handler *RemoteOperationsHandler) HandleOperationHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var input remoteoperations.AgentAttemptInput
	if err := decodeOperation(r, &input); err != nil {
		operationResponse(w, nil, err)
		return
	}
	cancel, err := handler.operations.Heartbeat(r.Context(), operationAttempt(r, input.Nonce))
	operationResponse(w, remoteoperations.HeartbeatResponse{CancelRequested: cancel}, err)
}

func (handler *RemoteOperationsHandler) HandleOperationComplete(w http.ResponseWriter, r *http.Request) {
	if !handler.admitAgent(w) {
		return
	}
	defer handler.releaseAgent()
	var input remoteoperations.AgentCompletionInput
	if err := decodeOperation(r, &input); err != nil {
		operationResponse(w, nil, err)
		return
	}
	defer clear(input.Result.Stdout)
	defer clear(input.Result.Stderr)
	err := handler.operations.Complete(r.Context(), remoteoperations.Completion{Attempt: operationAttempt(r, input.Nonce), Result: input.Result})
	operationResponse(w, remoteoperations.CompletionReceipt{Acknowledged: true}, err)
}
