package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

type GeoIPService interface {
	Settings(context.Context, geoip.Actor) (model.GeoIPSettings, error)
	SaveSettings(context.Context, geoip.Actor, model.GeoIPSettingsWrite) (model.GeoIPSettings, error)
	Location(context.Context, geoip.Actor, string) (model.NodeLocation, error)
	SaveLocation(context.Context, geoip.Actor, geoip.LocationMutation) (model.NodeLocation, error)
	Refresh(context.Context, geoip.Actor, string) error
	InstallMMDB(context.Context, geoip.Actor, geoip.MMDBUpload) (model.GeoIPSettings, error)
}

type GeoIPHandler struct{ service GeoIPService }

func NewGeoIPHandler(service GeoIPService) *GeoIPHandler { return &GeoIPHandler{service: service} }

func geoIPActor(w http.ResponseWriter, r *http.Request) (geoip.Actor, bool) {
	operationHeaders(w)
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "session_required")
		return geoip.Actor{}, false
	}
	return geoip.Actor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context())}, true
}

func geoIPResponse(w http.ResponseWriter, value any, err error) {
	operationHeaders(w)
	if err == nil {
		JSONResponse(w, http.StatusOK, value)
		return
	}
	status, code := http.StatusServiceUnavailable, "geoip_unavailable"
	switch {
	case errors.Is(err, geoip.ErrInvalid):
		status, code = http.StatusBadRequest, "geoip_invalid"
	case errors.Is(err, geoip.ErrForbidden):
		status, code = http.StatusForbidden, "geoip_forbidden"
	case errors.Is(err, geoip.ErrNotFound):
		status, code = http.StatusNotFound, "geoip_not_found"
	case errors.Is(err, geoip.ErrConflict):
		status, code = http.StatusConflict, "geoip_conflict"
	}
	JSONError(w, status, code)
}

func decodeGeoIP(r *http.Request, value any) error {
	const maximumRequestBytes = 16 * 1024
	data, err := io.ReadAll(io.LimitReader(r.Body, maximumRequestBytes+1))
	if err != nil || len(data) > maximumRequestBytes {
		return geoip.ErrInvalid
	}
	defer clear(data)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return geoip.ErrInvalid
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return geoip.ErrInvalid
	}
	return nil
}

func (h *GeoIPHandler) HandleSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	value, err := h.service.Settings(r.Context(), actor)
	geoIPResponse(w, value, err)
}

func (h *GeoIPHandler) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	var input model.GeoIPSettingsWrite
	if err := decodeGeoIP(r, &input); err != nil {
		geoIPResponse(w, nil, err)
		return
	}
	value, err := h.service.SaveSettings(r.Context(), actor, input)
	geoIPResponse(w, value, err)
}

func (h *GeoIPHandler) HandleLocation(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	value, err := h.service.Location(r.Context(), actor, r.PathValue("id"))
	geoIPResponse(w, value, err)
}

func (h *GeoIPHandler) HandleSaveLocation(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	var input model.LocationWrite
	if err := decodeGeoIP(r, &input); err != nil {
		geoIPResponse(w, nil, err)
		return
	}
	value, err := h.service.SaveLocation(r.Context(), actor, geoip.LocationMutation{NodeID: r.PathValue("id"), LocationWrite: input})
	geoIPResponse(w, value, err)
}

func (h *GeoIPHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	err := h.service.Refresh(r.Context(), actor, r.PathValue("id"))
	geoIPResponse(w, map[string]bool{"scheduled": err == nil}, err)
}

func (h *GeoIPHandler) HandleMMDBUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(r.Header.Get("If-Match"), 10, 64)
	if err != nil || revision < 1 {
		geoIPResponse(w, nil, geoip.ErrInvalid)
		return
	}
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		geoIPResponse(w, nil, geoip.ErrInvalid)
		return
	}
	input := geoip.MMDBUpload{Body: http.MaxBytesReader(w, r.Body, geoip.MaximumMMDBBytes), ExpectedRevision: revision}
	value, err := h.service.InstallMMDB(r.Context(), actor, input)
	geoIPResponse(w, value, err)
}
