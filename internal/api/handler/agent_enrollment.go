package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/store"
)

const (
	enrollmentBudgetKeys   = 1024
	issueRequestsPerMinute = 10
	issueGlobalRequests    = 100
	issueConcurrency       = 8
	joinRequestsPerMinute  = 60
	joinGlobalRequests     = 600
	joinConcurrency        = 16
)

type AgentEnrollmentOperations interface {
	CreateEnrollmentToken(context.Context, store.EnrollmentTokenInput) (store.EnrollmentToken, error)
	ConsumeEnrollmentToken(context.Context, store.EnrollmentConsumeInput) (agentjoin.Result, error)
}

type AgentEnrollmentHandler struct {
	store                   AgentEnrollmentOperations
	now                     func() time.Time
	generate                func() (agentjoin.Token, error)
	issueBudget, joinBudget *enrollmentBudget
}

func NewAgentEnrollmentHandler(store AgentEnrollmentOperations) *AgentEnrollmentHandler {
	return &AgentEnrollmentHandler{store: store, now: time.Now, generate: agentjoin.GenerateToken,
		issueBudget: newEnrollmentBudget(enrollmentBudgetPolicy{perIP: issueRequestsPerMinute, total: issueGlobalRequests, concurrent: issueConcurrency, keys: enrollmentBudgetKeys}),
		joinBudget:  newEnrollmentBudget(enrollmentBudgetPolicy{perIP: joinRequestsPerMinute, total: joinGlobalRequests, concurrent: joinConcurrency, keys: enrollmentBudgetKeys}),
	}
}

func (h *AgentEnrollmentHandler) HandleIssue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return
	}
	release, _, ok := h.admit(w, r, h.issueBudget)
	if !ok {
		return
	}
	defer release()
	var body agentjoin.IssueRequest
	if !parseEnrollmentJSON(w, r, &body) {
		return
	}
	origin, err := agentjoin.NormalizeServerURL(body.ServerURL, body.AllowHTTP)
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := h.generate()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to generate enrollment credential")
		return
	}
	now := h.now().UTC()
	issued, err := h.store.CreateEnrollmentToken(r.Context(), store.EnrollmentTokenInput{
		Hash: token.Hash, Prefix: token.Prefix, ServerURL: origin, AllowHTTP: strings.HasPrefix(origin, "http://"),
		CreatorID: principal.User.ID, CreatedAt: now, ExpiresAt: now.Add(agentjoin.TokenLifetime),
	})
	if errors.Is(err, store.ErrEnrollmentCapacity) {
		enrollmentLimited(w)
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to issue enrollment credential")
		return
	}
	JSONResponse(w, http.StatusCreated, agentjoin.IssueResult{ID: issued.ID, Token: token.Plaintext,
		ServerURL: issued.ServerURL, ExpiresAt: issued.ExpiresAt, Command: agentjoin.JoinCommand(issued.ServerURL, token.Plaintext),
	})
}

func (h *AgentEnrollmentHandler) HandleJoin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, sourceIP, ok := h.admit(w, r, h.joinBudget)
	if !ok {
		return
	}
	defer release()
	token, ok := enrollmentBearer(r.Header.Get("Authorization"))
	if !ok {
		JSONError(w, http.StatusUnauthorized, "invalid join credential")
		return
	}
	var body agentjoin.Request
	if !parseEnrollmentJSON(w, r, &body) {
		return
	}
	body.NodeName = strings.TrimSpace(body.NodeName)
	if err := body.Validate(); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.store.ConsumeEnrollmentToken(r.Context(), store.EnrollmentConsumeInput{
		TokenHash: agentjoin.DigestToken(token), Request: body, SourceIP: sourceIP,
		Secure: middleware.IsSecure(r), Now: h.now, AuditRequestID: middleware.RequestID(r.Context()),
	})
	if err != nil {
		enrollmentJoinError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, result)
}

func enrollmentJoinError(w http.ResponseWriter, err error) {
	if respondAlertScopeCapacity(w, err) {
		return
	}
	switch {
	case errors.Is(err, store.ErrEnrollmentUnauthorized):
		JSONError(w, http.StatusUnauthorized, "join credential is invalid, expired, revoked or already used")
	case errors.Is(err, store.ErrEnrollmentIdentityUnavailable):
		JSONError(w, http.StatusForbidden, agentjoin.AgentIdentityUnavailableMessage)
	case errors.Is(err, store.ErrNodeNameConflict):
		JSONError(w, http.StatusConflict, agentjoin.NodeNameConflictMessage)
	case errors.Is(err, store.ErrAgentTokenConflict):
		JSONError(w, http.StatusConflict, "agent credential already belongs to a node")
	case errors.Is(err, store.ErrEnrollmentSource):
		JSONError(w, http.StatusBadRequest, "invalid client source address")
	default:
		JSONError(w, http.StatusInternalServerError, "failed to enroll node")
	}
}

func enrollmentBearer(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	if err := agentjoin.ValidateToken(parts[1]); err != nil {
		return "", false
	}
	return parts[1], true
}
