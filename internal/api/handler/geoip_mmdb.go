package handler

import (
	"context"
	"net/http"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

type GeoIPDatabaseUpdater interface {
	UpdateMMDB(context.Context, geoip.Actor, geoip.MMDBUpdate) (model.GeoIPSettings, error)
	RollbackMMDB(context.Context, geoip.Actor, int64) (model.GeoIPSettings, error)
}

func (h *GeoIPHandler) HandleMMDBUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	service, ok := h.service.(GeoIPDatabaseUpdater)
	if !ok {
		geoIPResponse(w, nil, geoip.ErrUnavailable)
		return
	}
	var input geoip.MMDBUpdate
	if err := decodeGeoIP(r, &input); err != nil {
		geoIPResponse(w, nil, err)
		return
	}
	value, err := service.UpdateMMDB(r.Context(), actor, input)
	geoIPResponse(w, value, err)
}

func (h *GeoIPHandler) HandleMMDBRollback(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	service, ok := h.service.(GeoIPDatabaseUpdater)
	if !ok {
		geoIPResponse(w, nil, geoip.ErrUnavailable)
		return
	}
	var input struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeGeoIP(r, &input); err != nil {
		geoIPResponse(w, nil, err)
		return
	}
	value, err := service.RollbackMMDB(r.Context(), actor, input.ExpectedRevision)
	geoIPResponse(w, value, err)
}
