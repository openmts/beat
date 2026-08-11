package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/maintenance"
	"github.com/beat/backend/internal/model"
)

type fakeMaintenanceOperations struct {
	overview  model.MaintenanceOverview
	startErr  error
	getErr    error
	updateErr error
}

func (service *fakeMaintenanceOperations) Overview(
	context.Context,
) (model.MaintenanceOverview, error) {
	return service.overview, service.getErr
}

func (service *fakeMaintenanceOperations) UpdateSettings(
	_ context.Context,
	settings model.MaintenanceSettings,
) (model.MaintenanceSettings, error) {
	if service.updateErr != nil {
		return model.MaintenanceSettings{}, service.updateErr
	}
	service.overview.Settings = settings
	return settings, nil
}

func (service *fakeMaintenanceOperations) StartManual() error { return service.startErr }

func TestMaintenanceHandler(t *testing.T) {
	service := &fakeMaintenanceOperations{overview: model.MaintenanceOverview{
		Settings: model.DefaultMaintenanceSettings(),
	}}
	handler := NewMaintenanceHandler(service)
	tests := []struct {
		name   string
		method string
		body   string
		handle http.HandlerFunc
		want   int
	}{
		{name: "get", method: http.MethodGet, handle: handler.HandleGet, want: http.StatusOK},
		{name: "update", method: http.MethodPut,
			body:   `{"retention_days":60,"auto_cleanup_enabled":true,"cleanup_hour_utc":4}`,
			handle: handler.HandleUpdate, want: http.StatusOK},
		{name: "invalid update", method: http.MethodPut,
			body:   `{"retention_days":0,"cleanup_hour_utc":4}`,
			handle: handler.HandleUpdate, want: http.StatusBadRequest},
		{name: "run", method: http.MethodPost, handle: handler.HandleRun, want: http.StatusAccepted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			test.handle(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestMaintenanceHandlerRejectsConcurrentRun(t *testing.T) {
	handler := NewMaintenanceHandler(&fakeMaintenanceOperations{startErr: maintenance.ErrAlreadyRunning})
	response := httptest.NewRecorder()
	handler.HandleRun(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}

	handler = NewMaintenanceHandler(&fakeMaintenanceOperations{startErr: errors.New("failed")})
	response = httptest.NewRecorder()
	handler.HandleRun(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestMaintenanceHandlerErrors(t *testing.T) {
	tests := []struct {
		name    string
		service *fakeMaintenanceOperations
		method  string
		body    string
		handle  func(*MaintenanceHandler) http.HandlerFunc
	}{
		{name: "get", service: &fakeMaintenanceOperations{getErr: errors.New("failed")},
			method: http.MethodGet, handle: func(handler *MaintenanceHandler) http.HandlerFunc { return handler.HandleGet }},
		{name: "invalid json", service: &fakeMaintenanceOperations{}, method: http.MethodPut,
			body: "{", handle: func(handler *MaintenanceHandler) http.HandlerFunc { return handler.HandleUpdate }},
		{name: "update", service: &fakeMaintenanceOperations{updateErr: errors.New("failed")},
			method: http.MethodPut,
			body:   `{"retention_days":30,"auto_cleanup_enabled":true,"cleanup_hour_utc":3}`,
			handle: func(handler *MaintenanceHandler) http.HandlerFunc { return handler.HandleUpdate }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewMaintenanceHandler(test.service)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, "/", strings.NewReader(test.body))
			test.handle(handler)(response, request)
			if response.Code == http.StatusOK {
				t.Fatalf("status = %d, want error", response.Code)
			}
		})
	}
}
