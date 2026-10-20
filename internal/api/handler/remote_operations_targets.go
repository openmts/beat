package handler

import (
	"net/http"

	"github.com/beat/backend/internal/remoteoperations"
)

func operationTarget(r *http.Request) remoteoperations.TargetRequest {
	return remoteoperations.TargetRequest{TaskID: r.PathValue("id"), TargetID: r.PathValue("target_id")}
}

func (handler *RemoteOperationsHandler) HandleRetryOperationTarget(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input remoteoperations.RetryInput
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		input.TargetRequest = operationTarget(r)
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.operations.RetryTarget(r.Context(), actor, input)
	})
}

func (handler *RemoteOperationsHandler) HandleOperationTargetOutput(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.TargetOutput(r.Context(), actor, operationTarget(r))
	})
}
