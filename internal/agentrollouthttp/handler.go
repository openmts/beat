package agentrollouthttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/api/middleware"
)

const MaximumJSONBytes = 2 << 20

const MaximumUploadBytes = 512 << 20

const maximumJSONBytes = MaximumJSONBytes

const transferTimeout = 10 * time.Minute

type Handler struct {
	service *agentrollout.Service
	uploads chan struct{}
}

func New(service *agentrollout.Service) *Handler {
	return &Handler{service: service, uploads: make(chan struct{}, 4)}
}

func headers(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func respond(w http.ResponseWriter, value any, err error) {
	headers(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	status, message := http.StatusOK, "success"
	if err != nil {
		status = errorStatus(err)
		message = "rollout request was rejected"
		value = nil
	}
	if errors.Is(err, agentrollout.ErrBusy) {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	encodeErr := json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}{status, message, value})
	_ = encodeErr
}

func errorStatus(err error) int {
	switch {
	case errors.Is(err, agentrollout.ErrInvalid), errors.Is(err, agentrollout.ErrManifest), errors.Is(err, agentrollout.ErrSignature):
		return 400
	case errors.Is(err, agentrollout.ErrForbidden):
		return 403
	case errors.Is(err, agentrollout.ErrRecentAuth):
		return 428
	case errors.Is(err, agentrollout.ErrNotFound), errors.Is(err, agentrollout.ErrDisabled):
		return 404
	case errors.Is(err, agentrollout.ErrConflict):
		return 409
	default:
		return 503
	}
}

func actor(r *http.Request) (agentrollout.Actor, error) {
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok {
		return agentrollout.Actor{}, agentrollout.ErrForbidden
	}
	return agentrollout.Actor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context()), IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()}, nil
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request, action func(agentrollout.Actor) (any, error)) {
	principal, err := actor(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	value, err := action(principal)
	respond(w, value, err)
}

func decode(r *http.Request, value any) error {
	if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
		return agentrollout.ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maximumJSONBytes+1))
	if err != nil || len(body) > maximumJSONBytes {
		return agentrollout.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return agentrollout.ErrInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return agentrollout.ErrInvalid
	}
	return nil
}

func page(r *http.Request) (agentrollout.PageQuery, error) {
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(values["cursor"]) > 1 || len(values["limit"]) > 1 {
		return agentrollout.PageQuery{}, agentrollout.ErrInvalid
	}
	query := agentrollout.PageQuery{Cursor: values.Get("cursor"), RolloutID: r.PathValue("rollout_id")}
	if text := values.Get("limit"); text != "" {
		limit, err := strconv.Atoi(text)
		if err != nil {
			return query, agentrollout.ErrInvalid
		}
		query.Limit = limit
	}
	return query, nil
}
