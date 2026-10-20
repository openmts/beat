package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

type geoIPStub struct{ err error }

func (s geoIPStub) Settings(context.Context, geoip.Actor) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{Provider: "disabled"}, s.err
}

func (s geoIPStub) SaveSettings(context.Context, geoip.Actor, model.GeoIPSettingsWrite) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, s.err
}

func (s geoIPStub) Location(context.Context, geoip.Actor, string) (model.NodeLocation, error) {
	return model.NodeLocation{}, s.err
}

func (s geoIPStub) SaveLocation(context.Context, geoip.Actor, geoip.LocationMutation) (model.NodeLocation, error) {
	return model.NodeLocation{}, s.err
}

func (s geoIPStub) Refresh(context.Context, geoip.Actor, string) error { return s.err }

func (s geoIPStub) InstallMMDB(context.Context, geoip.Actor, geoip.MMDBUpload) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, s.err
}

func (s geoIPStub) UpdateMMDB(context.Context, geoip.Actor, geoip.MMDBUpdate) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, s.err
}

func (s geoIPStub) RollbackMMDB(context.Context, geoip.Actor, int64) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, s.err
}

func geoIPRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/geoip", strings.NewReader(body))
	request.SetPathValue("id", "node")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("If-Match", "1")
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{User: model.AdminUser{ID: "owner"}, Session: model.AdminSession{ID: "session"}}))
}

func TestGeoIPEndpointsRequireSessionAndReturnSafeResponses(t *testing.T) {
	handler := NewGeoIPHandler(geoIPStub{})
	actions := []http.HandlerFunc{handler.HandleSettings, handler.HandleSaveSettings, handler.HandleLocation, handler.HandleSaveLocation, handler.HandleRefresh, handler.HandleMMDBUpload, handler.HandleMMDBUpdate, handler.HandleMMDBRollback}
	for _, action := range actions {
		anonymous := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`))
		response := httptest.NewRecorder()
		action(response, anonymous)
		if response.Code != http.StatusUnauthorized {
			t.Fatal(response.Code)
		}
		response = httptest.NewRecorder()
		action(response, geoIPRequest(`{}`))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}

func TestGeoIPFailureCodesAndStrictBodies(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{geoip.ErrInvalid, 400}, {geoip.ErrForbidden, 403}, {geoip.ErrNotFound, 404}, {geoip.ErrConflict, 409}, {errors.New("PRIVATE_SOURCE"), 503}} {
		response := httptest.NewRecorder()
		NewGeoIPHandler(geoIPStub{err: test.err}).HandleSettings(response, geoIPRequest(`{}`))
		if response.Code != test.status || strings.Contains(response.Body.String(), "PRIVATE") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	handler := NewGeoIPHandler(geoIPStub{})
	for _, action := range []http.HandlerFunc{handler.HandleSaveSettings, handler.HandleSaveLocation, handler.HandleMMDBUpdate, handler.HandleMMDBRollback} {
		for _, body := range []string{`{"unknown":1}`, `{} {}`, `{`, strings.Repeat(" ", 16385)} {
			response := httptest.NewRecorder()
			action(response, geoIPRequest(body))
			if response.Code != 400 {
				t.Fatal(response.Code)
			}
		}
	}
	request := geoIPRequest(`{}`)
	request.Header.Del("If-Match")
	response := httptest.NewRecorder()
	handler.HandleMMDBUpload(response, request)
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
	request = geoIPRequest(`{}`)
	request.Header.Set("Content-Type", "multipart/form-data")
	response = httptest.NewRecorder()
	handler.HandleMMDBUpload(response, request)
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
}

type geoIPNoUpdater struct{ GeoIPService }

func TestGeoIPMissingOptionalUpdater(t *testing.T) {
	handler := NewGeoIPHandler(geoIPNoUpdater{GeoIPService: geoIPStub{}})
	for _, action := range []http.HandlerFunc{handler.HandleMMDBUpdate, handler.HandleMMDBRollback} {
		response := httptest.NewRecorder()
		action(response, geoIPRequest(`{}`))
		if response.Code != 503 {
			t.Fatal(response.Code)
		}
	}
}
