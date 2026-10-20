package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"

	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/api/middleware"
)

func (h *AgentEnrollmentHandler) admit(w http.ResponseWriter, r *http.Request, budget *enrollmentBudget) (func(), string, bool) {
	address, err := netip.ParseAddr(middleware.ClientIP(r))
	if err != nil || address.Zone() != "" {
		JSONError(w, http.StatusBadRequest, "invalid client source address")
		return nil, "", false
	}
	key := address.Unmap().String()
	release, ok := budget.acquire(key, h.now())
	if !ok {
		enrollmentLimited(w)
		return nil, "", false
	}
	return release, key, true
}

func enrollmentLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	JSONError(w, http.StatusTooManyRequests, "enrollment request limit reached; try again later")
}

func parseEnrollmentJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		JSONError(w, http.StatusUnsupportedMediaType, "application/json is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, agentjoin.MaximumBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		enrollmentJSONError(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		enrollmentJSONError(w, err)
		return false
	}
	return true
}

func enrollmentJSONError(w http.ResponseWriter, err error) {
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		JSONError(w, http.StatusRequestEntityTooLarge, "enrollment request is too large")
		return
	}
	JSONError(w, http.StatusBadRequest, "invalid enrollment JSON request")
}
