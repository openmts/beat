package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

const remoteMMDBInstallWriteTimeout = 2 * time.Minute

type GeoIPRemoteMMDB interface {
	RemoteMMDBRelease(context.Context, geoip.Actor) (geoip.RemoteMMDBRelease, error)
	InstallRemoteMMDB(context.Context, geoip.Actor, geoip.RemoteMMDBInstall) (model.GeoIPSettings, error)
}

func (h *GeoIPHandler) HandleRemoteMMDBRelease(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	service, ok := h.service.(GeoIPRemoteMMDB)
	if !ok {
		geoIPResponse(w, nil, geoip.ErrUnavailable)
		return
	}
	value, err := service.RemoteMMDBRelease(r.Context(), actor)
	geoIPResponse(w, value, err)
}

func (h *GeoIPHandler) HandleRemoteMMDBInstall(w http.ResponseWriter, r *http.Request) {
	actor, ok := geoIPActor(w, r)
	if !ok {
		return
	}
	service, ok := h.service.(GeoIPRemoteMMDB)
	if !ok {
		geoIPResponse(w, nil, geoip.ErrUnavailable)
		return
	}
	var input geoip.RemoteMMDBInstall
	if err := decodeGeoIP(r, &input); err != nil {
		geoIPResponse(w, nil, err)
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(remoteMMDBInstallWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		geoIPResponse(w, nil, geoip.ErrUnavailable)
		return
	}
	value, err := service.InstallRemoteMMDB(r.Context(), actor, input)
	geoIPResponse(w, value, err)
}
