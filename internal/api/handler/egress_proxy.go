package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/egressproxy"
)

type EgressProxyOperations interface {
	Get(context.Context) (egressproxy.Settings, error)
	Update(context.Context, egressproxy.Update) (egressproxy.Settings, error)
	Probe(context.Context) (egressproxy.ProbeResult, error)
}

type EgressProxyHandler struct {
	service EgressProxyOperations
}

func NewEgressProxyHandler(service EgressProxyOperations) *EgressProxyHandler {
	return &EgressProxyHandler{service: service}
}

func (handler *EgressProxyHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	value, err := handler.service.Get(r.Context())
	if err != nil {
		writeEgressProxyError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, value)
}

func (handler *EgressProxyHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var input egressproxy.Update
	if err := ParseJSONStrict(r, &input); err != nil {
		WriteBodyError(w, err)
		return
	}
	value, err := handler.service.Update(r.Context(), input)
	if err != nil {
		writeEgressProxyError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, value)
}

func (handler *EgressProxyHandler) HandleProbe(w http.ResponseWriter, r *http.Request) {
	value, err := handler.service.Probe(r.Context())
	if err != nil {
		writeEgressProxyError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, value)
}

func writeEgressProxyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, egressproxy.ErrInvalid):
		JSONError(w, http.StatusBadRequest, "invalid egress proxy settings")
	case errors.Is(err, egressproxy.ErrConflict):
		JSONError(w, http.StatusConflict, "egress proxy settings changed")
	case errors.Is(err, egressproxy.ErrDisabled):
		JSONError(w, http.StatusConflict, "egress proxy is disabled")
	case errors.Is(err, egressproxy.ErrProbeTimeout):
		JSONError(w, http.StatusGatewayTimeout, "egress proxy probe timed out")
	case errors.Is(err, egressproxy.ErrProbeFailed):
		JSONError(w, http.StatusBadGateway, "egress proxy probe failed")
	default:
		JSONError(w, http.StatusInternalServerError, "egress proxy is unavailable")
	}
}
