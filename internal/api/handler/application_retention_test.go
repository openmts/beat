package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/applicationgc"
	"github.com/beat/backend/internal/model"
)

type fakeApplicationRetention struct {
	overview   model.ApplicationRetentionOverview
	getErr     error
	updateErr  error
	triggerErr error
}

func (f *fakeApplicationRetention) Overview(context.Context) (model.ApplicationRetentionOverview, error) {
	return f.overview, f.getErr
}

func (f *fakeApplicationRetention) UpdateSettings(
	_ context.Context,
	settings model.ApplicationRetentionSettings,
) (model.ApplicationRetentionSettings, error) {
	if f.updateErr != nil {
		return model.ApplicationRetentionSettings{}, f.updateErr
	}
	return settings, nil
}

func (f *fakeApplicationRetention) TriggerManual() error { return f.triggerErr }

func TestApplicationRetentionHandlerLifecycle(t *testing.T) {
	service := &fakeApplicationRetention{overview: model.ApplicationRetentionOverview{
		Settings:             model.DefaultApplicationRetentionSettings(),
		Status:               model.ApplicationRetentionStatus{LastStatus: model.ApplicationRetentionStatusNever},
		SessionRetentionDays: model.ApplicationSessionRetentionDays,
	}}
	handler := NewApplicationRetentionHandler(service)
	recorder := httptest.NewRecorder()
	handler.HandleGet(recorder, jsonRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("alert_event_days")) {
		t.Fatalf("get status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	body := `{"alert_event_days":30,"audit_event_days":60,"cleanup_hour_utc":5,"auto_cleanup_enabled":false}`
	recorder = httptest.NewRecorder()
	handler.HandleUpdate(recorder, jsonRequest(http.MethodPut, "/", bytes.NewBufferString(body)))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("cleanup_hour_utc")) {
		t.Fatalf("update status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.HandleRun(recorder, jsonRequest(http.MethodPost, "/run", nil))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("run status = %d", recorder.Code)
	}
}

func TestApplicationRetentionHandlerRejectsInvalidInput(t *testing.T) {
	handler := NewApplicationRetentionHandler(&fakeApplicationRetention{})
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"invalid json", "{", http.StatusBadRequest},
		{"out of range days", `{"alert_event_days":0,"audit_event_days":60,"cleanup_hour_utc":5}`, http.StatusBadRequest},
		{"out of range hour", `{"alert_event_days":30,"audit_event_days":60,"cleanup_hour_utc":24}`, http.StatusBadRequest},
	}
	for _, test := range cases {
		recorder := httptest.NewRecorder()
		handler.HandleUpdate(recorder, jsonRequest(http.MethodPut, "/", bytes.NewBufferString(test.body)))
		if recorder.Code != test.status {
			t.Fatalf("%s status = %d, want %d", test.name, recorder.Code, test.status)
		}
	}
}

func TestApplicationRetentionHandlerErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		service *fakeApplicationRetention
		call    func(*ApplicationRetentionHandler, *httptest.ResponseRecorder)
		status  int
	}{
		{"get failure", &fakeApplicationRetention{getErr: errors.New("boom")},
			func(h *ApplicationRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleGet(w, jsonRequest(http.MethodGet, "/", nil))
			}, http.StatusInternalServerError},
		{"update failure", &fakeApplicationRetention{updateErr: errors.New("boom")},
			func(h *ApplicationRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleUpdate(w, jsonRequest(http.MethodPut, "/", bytes.NewBufferString(
					`{"alert_event_days":30,"audit_event_days":60,"cleanup_hour_utc":5}`)))
			}, http.StatusInternalServerError},
		{"already running", &fakeApplicationRetention{triggerErr: applicationgc.ErrAlreadyRunning},
			func(h *ApplicationRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleRun(w, jsonRequest(http.MethodPost, "/run", nil))
			}, http.StatusConflict},
		{"trigger failure", &fakeApplicationRetention{triggerErr: errors.New("boom")},
			func(h *ApplicationRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleRun(w, jsonRequest(http.MethodPost, "/run", nil))
			}, http.StatusInternalServerError},
	}
	for _, test := range cases {
		recorder := httptest.NewRecorder()
		test.call(NewApplicationRetentionHandler(test.service), recorder)
		if recorder.Code != test.status {
			t.Fatalf("%s status = %d, want %d", test.name, recorder.Code, test.status)
		}
	}
}
