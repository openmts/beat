package handler

import (
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/diagnostics"
)

func (handler *DiagnosticHandler) HandleDiagnosticPolicy(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return handler.diagnostics.Policy(r.Context(), actor)
	})
}

func (handler *DiagnosticHandler) HandleSaveDiagnosticPolicy(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		var request diagnosticPolicyRequest
		if err := decodeDiagnostic(r, &request); err != nil {
			return nil, diagnostics.ErrInvalid
		}
		return handler.diagnostics.SavePolicy(r.Context(), actor, request.policy())
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticTargets(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return handler.diagnostics.ListTargets(r.Context(), actor)
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticTarget(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return handler.diagnostics.GetTarget(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *DiagnosticHandler) HandleCreateDiagnosticTarget(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		var request diagnosticTargetRequest
		if err := decodeDiagnostic(r, &request); err != nil {
			return nil, diagnostics.ErrInvalid
		}
		return handler.diagnostics.SaveTarget(r.Context(), actor, diagnostics.TargetChange{Input: request.input()})
	})
}

func (handler *DiagnosticHandler) HandleSaveDiagnosticTarget(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		var request diagnosticTargetRequest
		if err := decodeDiagnostic(r, &request); err != nil {
			return nil, diagnostics.ErrInvalid
		}
		change := diagnostics.TargetChange{ID: r.PathValue("id"), ExpectedRevision: request.Revision, Input: request.input()}
		return handler.diagnostics.SaveTarget(r.Context(), actor, change)
	})
}

func (handler *DiagnosticHandler) HandleDeleteDiagnosticTarget(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return acknowledged{}, handler.diagnostics.DeleteTarget(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticCapabilities(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return handler.diagnostics.Capabilities(r.Context(), actor)
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticJobs(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		limit, err := diagnosticLimit(r)
		if err != nil {
			return nil, err
		}
		for key := range r.URL.Query() {
			if key != "limit" && key != "state" && key != "node_id" && key != "target_id" {
				return nil, diagnostics.ErrInvalid
			}
		}
		filter := diagnostics.JobFilter{Limit: limit, State: r.URL.Query().Get("state"),
			SourceNode: r.URL.Query().Get("node_id"), TargetID: r.URL.Query().Get("target_id")}
		return handler.diagnostics.ListJobs(r.Context(), actor, filter)
	})
}

func (handler *DiagnosticHandler) HandleCreateDiagnosticJob(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		var request diagnosticJobRequest
		if err := decodeDiagnostic(r, &request); err != nil {
			return nil, diagnostics.ErrInvalid
		}
		input := diagnostics.JobInput{SourceNodeID: request.SourceNodeID, TargetID: request.TargetID,
			Kind: request.Kind, Protocol: request.Protocol, Direction: request.Direction, IPFamily: request.IPFamily,
			Probes: request.Probes, HopLimit: request.HopLimit, DurationSeconds: request.DurationSeconds,
			ParallelStreams: request.ParallelStreams, BitrateMbps: request.BitrateMbps, Confirmation: request.Confirmation}
		return handler.diagnostics.CreateJob(r.Context(), actor, input)
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticJob(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return handler.diagnostics.Job(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *DiagnosticHandler) HandleCancelDiagnosticJob(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return acknowledged{}, handler.diagnostics.CancelJob(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *DiagnosticHandler) HandleRetryDiagnosticJob(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return handler.diagnostics.RetryJob(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *DiagnosticHandler) HandleDeleteDiagnosticJob(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		return acknowledged{}, handler.diagnostics.DeleteJob(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *DiagnosticHandler) HandleDiagnosticResults(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor diagnostics.Actor) (any, error) {
		hop, err := diagnosticInteger(r, "hop")
		if err != nil {
			return nil, err
		}
		limit, err := diagnosticLimit(r)
		if err != nil {
			return nil, err
		}
		for key := range r.URL.Query() {
			if !diagnosticResultKeys[key] {
				return nil, diagnostics.ErrInvalid
			}
		}
		query := diagnostics.ResultQuery{JobID: r.PathValue("id"), Metric: r.URL.Query().Get("metric"),
			Stat: r.URL.Query().Get("stat"), Direction: r.URL.Query().Get("direction"),
			Protocol: r.URL.Query().Get("protocol"), Role: r.URL.Query().Get("role"), Hop: hop, Limit: limit}
		return handler.diagnostics.Results(r.Context(), actor, query)
	})
}

var diagnosticResultKeys = map[string]bool{"metric": true, "stat": true, "direction": true, "protocol": true,
	"role": true, "hop": true, "limit": true}

func diagnosticInteger(r *http.Request, key string) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, diagnostics.ErrInvalid
	}
	return value, nil
}

type acknowledged struct {
	Acknowledged bool `json:"acknowledged"`
}
