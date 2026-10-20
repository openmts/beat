package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/remoteoperations"
)

type RemoteOperations interface {
	Status(context.Context, remoteoperations.Actor) (remoteoperations.Status, error)
	TaskContent(context.Context, remoteoperations.Actor, string) (remoteoperations.TaskContent, error)
	ForceDeleteNode(context.Context, remoteoperations.Actor, remoteoperations.ForceNodeDelete) error
	ListTaskPage(context.Context, remoteoperations.Actor, remoteoperations.TaskPageQuery) (remoteoperations.TaskPage, error)
	ExecutablePolicies(context.Context, remoteoperations.Actor) ([]remoteoperations.PolicyInput, error)
	GetPolicy(context.Context, remoteoperations.Actor, string) (remoteoperations.PolicyInput, error)
	DisablePolicy(context.Context, remoteoperations.Actor, string) error
	DeleteSnippet(context.Context, remoteoperations.Actor, string) error
	RetryTarget(context.Context, remoteoperations.Actor, remoteoperations.RetryInput) error
	TargetOutput(context.Context, remoteoperations.Actor, remoteoperations.TargetRequest) (remoteoperations.TargetOutput, error)
	SavePolicy(context.Context, remoteoperations.Actor, remoteoperations.PolicyInput) (string, error)
	ListPolicies(context.Context, remoteoperations.Actor) ([]remoteoperations.PolicyInput, error)
	SaveSnippet(context.Context, remoteoperations.Actor, remoteoperations.SnippetInput) (remoteoperations.SnippetVersion, error)
	ListSnippets(context.Context, remoteoperations.Actor) ([]remoteoperations.SnippetVersion, error)
	SnippetContent(context.Context, remoteoperations.Actor, remoteoperations.SnippetVersion) ([]byte, error)
	ApproveSnippet(context.Context, remoteoperations.Actor, remoteoperations.SnippetVersion) error
	CreateTask(context.Context, remoteoperations.Actor, remoteoperations.TaskInput) (string, error)
	ListTasks(context.Context, remoteoperations.Actor) ([]remoteoperations.TaskSummary, error)
	GetTask(context.Context, remoteoperations.Actor, string) (remoteoperations.TaskSummary, error)
	CancelTask(context.Context, remoteoperations.Actor, string) error
	Advertise(context.Context, remoteoperations.NodeProof, remoteoperations.Policy) (string, error)
	Lease(context.Context, remoteoperations.LeaseRequest) (*remoteoperations.Lease, error)
	Start(context.Context, remoteoperations.AttemptRequest) error
	Heartbeat(context.Context, remoteoperations.AttemptRequest) (bool, error)
	Complete(context.Context, remoteoperations.Completion) error
}

type RemoteOperationsHandler struct {
	operations RemoteOperations
	agentSlots chan struct{}
}

func NewRemoteOperationsHandler(operations RemoteOperations) *RemoteOperationsHandler {
	return &RemoteOperationsHandler{operations: operations, agentSlots: make(chan struct{}, 16)}
}

type operationAdminAction func(remoteoperations.Actor) (any, error)

func (handler *RemoteOperationsHandler) admin(w http.ResponseWriter, r *http.Request, action operationAdminAction) {
	operationHeaders(w)
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return
	}
	value, err := action(remoteoperations.Actor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context()), IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()})
	operationResponse(w, value, err)
}

func operationHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func operationResponse(w http.ResponseWriter, value any, err error) {
	operationHeaders(w)
	if err == nil {
		JSONResponse(w, http.StatusOK, value)
		return
	}
	status := http.StatusServiceUnavailable
	message := "operation request was rejected"
	switch {
	case errors.Is(err, remoteoperations.ErrCorrupt):
		// 客户端声明的内容与自己上传的字节不符：这是内容错误而不是服务不可用，
		// 因此与其它 4xx 一样按冲突回答，并给出可执行的原因。
		status = http.StatusConflict
		message = "uploaded content does not match the declared digest"
	case errors.Is(err, remoteoperations.ErrBusy):
		status = http.StatusServiceUnavailable
		w.Header().Set("Retry-After", "1")
	case errors.Is(err, remoteoperations.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, remoteoperations.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, remoteoperations.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, remoteoperations.ErrNotFound):
		status = http.StatusNotFound
	}
	JSONError(w, status, message)
}

func (handler *RemoteOperationsHandler) HandleOperationPolicies(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.ListPolicies(r.Context(), actor)
	})
}

func (handler *RemoteOperationsHandler) HandleSaveOperationPolicy(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input remoteoperations.PolicyInput
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		if id := r.PathValue("id"); id != "" {
			input.ID = id
		}
		id, err := handler.operations.SavePolicy(r.Context(), actor, input)
		return map[string]string{"id": id}, err
	})
}

func (handler *RemoteOperationsHandler) HandleOperationTasks(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return nil, remoteoperations.ErrInvalid
			}
			limit = value
		}
		for key := range r.URL.Query() {
			if key != "limit" && key != "cursor" {
				return nil, remoteoperations.ErrInvalid
			}
		}
		return handler.operations.ListTaskPage(r.Context(), actor, remoteoperations.TaskPageQuery{Cursor: r.URL.Query().Get("cursor"), Limit: limit})
	})
}

func (handler *RemoteOperationsHandler) HandleOperationTask(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.GetTask(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteOperationsHandler) HandleCreateOperationTask(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input remoteoperations.TaskInput
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		id, err := handler.operations.CreateTask(r.Context(), actor, input)
		return map[string]string{"id": id}, err
	})
}

func (handler *RemoteOperationsHandler) HandleCancelOperationTask(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.operations.CancelTask(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteOperationsHandler) HandleOperationPolicy(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.GetPolicy(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteOperationsHandler) HandleDisableOperationPolicy(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.operations.DisablePolicy(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteOperationsHandler) HandleDeleteOperationSnippet(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.operations.DeleteSnippet(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteOperationsHandler) HandleExecutableOperationPolicies(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.ExecutablePolicies(r.Context(), actor)
	})
}

func (handler *RemoteOperationsHandler) HandleForceDeleteOperationNode(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input remoteoperations.ForceNodeDelete
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		input.NodeID = r.PathValue("id")
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.operations.ForceDeleteNode(r.Context(), actor, input)
	})
}

func (handler *RemoteOperationsHandler) HandleOperationTaskContent(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.operations.TaskContent(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteOperationsHandler) HandleOperationStatus(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) { return handler.operations.Status(r.Context(), actor) })
}

func (handler *RemoteOperationsHandler) admitAgent(w http.ResponseWriter) bool {
	select {
	case handler.agentSlots <- struct{}{}:
		return true
	default:
		operationHeaders(w)
		JSONError(w, http.StatusTooManyRequests, "operation request capacity is exhausted")
		return false
	}
}

func (handler *RemoteOperationsHandler) releaseAgent() { <-handler.agentSlots }
