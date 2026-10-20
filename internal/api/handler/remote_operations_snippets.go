package handler

import (
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/remoteoperations"
)

func (handler *RemoteOperationsHandler) HandleOperationSnippets(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.ListSnippets(r.Context(), actor)
	})
}

func (handler *RemoteOperationsHandler) HandleSaveOperationSnippet(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input remoteoperations.SnippetInput
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		defer clear(input.Content)
		if id := r.PathValue("id"); id != "" {
			input.ID = id
		}
		return handler.operations.SaveSnippet(r.Context(), actor, input)
	})
}

func (handler *RemoteOperationsHandler) HandleApproveOperationSnippet(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input struct {
			Version int `json:"version"`
		}
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.operations.ApproveSnippet(r.Context(), actor, remoteoperations.SnippetVersion{ID: r.PathValue("id"), Version: input.Version})
	})
}

func (handler *RemoteOperationsHandler) HandleOperationSnippetContent(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		version, err := strconv.Atoi(r.URL.Query().Get("version"))
		if err != nil || version < 1 {
			return nil, remoteoperations.ErrInvalid
		}
		content, err := handler.operations.SnippetContent(r.Context(), actor, remoteoperations.SnippetVersion{ID: r.PathValue("id"), Version: version})
		if err != nil {
			return nil, err
		}
		defer clear(content)
		return map[string]string{"content": string(content)}, nil
	})
}
