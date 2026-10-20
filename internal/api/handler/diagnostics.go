package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/diagnostics"
	"github.com/beat/backend/internal/model"
)

// Diagnostics 是诊断控制面在 HTTP 层的全部依赖；实现由服务端提供。
type Diagnostics interface {
	Policy(context.Context, diagnostics.Actor) (model.DiagnosticPolicy, error)
	SavePolicy(context.Context, diagnostics.Actor, model.DiagnosticPolicy) (model.DiagnosticPolicy, error)
	ListTargets(context.Context, diagnostics.Actor) ([]model.DiagnosticTarget, error)
	GetTarget(context.Context, diagnostics.Actor, string) (model.DiagnosticTarget, error)
	SaveTarget(context.Context, diagnostics.Actor, diagnostics.TargetChange) (model.DiagnosticTarget, error)
	DeleteTarget(context.Context, diagnostics.Actor, string) error
	Capabilities(context.Context, diagnostics.Actor) ([]diagnostics.NodeCapability, error)
	ListJobs(context.Context, diagnostics.Actor, diagnostics.JobFilter) ([]model.DiagnosticJob, error)
	Job(context.Context, diagnostics.Actor, string) (diagnostics.JobDetail, error)
	CreateJob(context.Context, diagnostics.Actor, diagnostics.JobInput) (model.DiagnosticJob, error)
	CancelJob(context.Context, diagnostics.Actor, string) error
	RetryJob(context.Context, diagnostics.Actor, string) (model.DiagnosticJob, error)
	DeleteJob(context.Context, diagnostics.Actor, string) error
	Results(context.Context, diagnostics.Actor, diagnostics.ResultQuery) (diagnostics.ResultSeries, error)
	Advertise(context.Context, diagnostics.NodeProof, diagnostics.CapabilityDocument) (string, error)
	Lease(context.Context, diagnostics.LeaseRequest) (*diagnostics.Lease, error)
	Start(context.Context, diagnostics.AttemptRequest) error
	Heartbeat(context.Context, diagnostics.AttemptRequest) (bool, error)
	Progress(context.Context, diagnostics.ProgressFrame) (int64, error)
	Complete(context.Context, diagnostics.Completion) error
}

type DiagnosticHandler struct {
	diagnostics Diagnostics
	agentSlots  chan struct{}
}

func NewDiagnosticHandler(service Diagnostics) *DiagnosticHandler {
	return &DiagnosticHandler{diagnostics: service, agentSlots: make(chan struct{}, 16)}
}

type diagnosticAction func(diagnostics.Actor) (any, error)

func (handler *DiagnosticHandler) admin(w http.ResponseWriter, r *http.Request, action diagnosticAction) {
	diagnosticHeaders(w)
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return
	}
	actor := diagnostics.Actor{UserID: principal.User.ID, Username: principal.User.Username, Role: string(principal.User.Role),
		SessionID: principal.Session.ID, SessionPrefix: principal.Session.TokenPrefix, RequestID: middleware.RequestID(r.Context()),
		IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()}
	value, err := action(actor)
	diagnosticResponse(w, value, err)
}

func diagnosticHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func diagnosticResponse(w http.ResponseWriter, value any, err error) {
	diagnosticHeaders(w)
	if err == nil {
		JSONResponse(w, http.StatusOK, value)
		return
	}
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, diagnostics.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, diagnostics.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, diagnostics.ErrCapability):
		status = http.StatusPreconditionFailed
	case errors.Is(err, diagnostics.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, diagnostics.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, diagnostics.ErrBudget):
		status = http.StatusTooManyRequests
	}
	JSONError(w, status, "diagnostic request was rejected")
}

func decodeDiagnostic(r *http.Request, target any) error { return decodeOperation(r, target) }

type diagnosticPolicyRequest struct {
	AllowedKinds             []string `json:"allowed_kinds"`
	Enabled                  bool     `json:"enabled"`
	AllowUDP                 bool     `json:"allow_udp"`
	AllowReverse             bool     `json:"allow_reverse"`
	AllowPrivateTargets      bool     `json:"allow_private_targets"`
	MaximumActiveJobs        int      `json:"maximum_active_jobs"`
	MaximumActiveJobsPerNode int      `json:"maximum_active_jobs_per_node"`
	DailyByteBudget          int64    `json:"daily_byte_budget"`
	MaximumIperfDuration     int      `json:"maximum_iperf_duration_seconds"`
	MaximumIperfBitrate      int      `json:"maximum_iperf_bitrate_mbps"`
	Revision                 int      `json:"revision"`
}

func (request diagnosticPolicyRequest) policy() model.DiagnosticPolicy {
	return model.DiagnosticPolicy{ID: "default", Enabled: request.Enabled, AllowedKinds: request.AllowedKinds,
		AllowUDP: request.AllowUDP, AllowReverse: request.AllowReverse, AllowPrivateTargets: request.AllowPrivateTargets,
		MaximumActiveJobs: request.MaximumActiveJobs, MaximumActiveJobsPerNode: request.MaximumActiveJobsPerNode,
		DailyByteBudget: request.DailyByteBudget, MaximumIperfDuration: request.MaximumIperfDuration,
		MaximumIperfBitrate: request.MaximumIperfBitrate, Revision: request.Revision}
}

type diagnosticTargetRequest struct {
	Name                  string   `json:"name"`
	Purpose               string   `json:"purpose"`
	Kinds                 []string `json:"kinds"`
	Address               string   `json:"address"`
	Port                  int      `json:"port"`
	IPFamily              string   `json:"ip_family"`
	Protocol              string   `json:"protocol"`
	Direction             string   `json:"direction"`
	PrivateAddress        bool     `json:"private_address"`
	AllowedAddresses      []string `json:"allowed_addresses"`
	MaximumBitrateMbps    int      `json:"maximum_bitrate_mbps"`
	MaximumDuration       int      `json:"maximum_duration_seconds"`
	MaximumParallelStream int      `json:"maximum_parallel_streams"`
	DailyByteBudget       int64    `json:"daily_byte_budget"`
	Enabled               bool     `json:"enabled"`
	Revision              int      `json:"revision"`
}

func (request diagnosticTargetRequest) input() model.DiagnosticTargetInput {
	return model.DiagnosticTargetInput{Name: request.Name, Purpose: request.Purpose, Kinds: request.Kinds,
		Address: request.Address, Port: request.Port, IPFamily: request.IPFamily, Protocol: request.Protocol,
		Direction: request.Direction, PrivateAddress: request.PrivateAddress, AllowedAddresses: request.AllowedAddresses,
		MaximumBitrateMbps: request.MaximumBitrateMbps, MaximumDuration: request.MaximumDuration,
		MaximumParallelStream: request.MaximumParallelStream, DailyByteBudget: request.DailyByteBudget,
		Enabled: request.Enabled}
}

type diagnosticJobRequest struct {
	SourceNodeID    string `json:"source_node_id"`
	TargetID        string `json:"target_id"`
	Kind            string `json:"kind"`
	Protocol        string `json:"protocol"`
	Direction       string `json:"direction"`
	IPFamily        string `json:"ip_family"`
	Probes          int    `json:"probes"`
	HopLimit        int    `json:"hop_limit"`
	DurationSeconds int    `json:"duration_seconds"`
	ParallelStreams int    `json:"parallel_streams"`
	BitrateMbps     int    `json:"bitrate_mbps"`
	Confirmation    string `json:"confirmation"`
}

func diagnosticLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, diagnostics.ErrInvalid
	}
	return value, nil
}
