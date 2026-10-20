package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/retentionadmin"
)

type fakeAgentRetention struct {
	settings  model.AgentRetentionSettings
	getErr    error
	updateErr error
	resetErr  error
	runErr    error
	updates   []model.AgentRetentionSettings
	resets    int
	runs      int
}

func (fake *fakeAgentRetention) Get(context.Context) (model.AgentRetentionSettings, error) {
	return fake.settings, fake.getErr
}

func (fake *fakeAgentRetention) Update(_ context.Context, settings model.AgentRetentionSettings) (model.AgentRetentionSettings, error) {
	if fake.updateErr != nil {
		return model.AgentRetentionSettings{}, fake.updateErr
	}
	fake.updates = append(fake.updates, settings)
	return settings, nil
}

func (fake *fakeAgentRetention) Reset(context.Context) (model.AgentRetentionSettings, error) {
	if fake.resetErr != nil {
		return model.AgentRetentionSettings{}, fake.resetErr
	}
	fake.resets++
	return fake.settings, nil
}

func (fake *fakeAgentRetention) Run(context.Context) (model.AgentRetentionSettings, error) {
	if fake.runErr != nil {
		return model.AgentRetentionSettings{}, fake.runErr
	}
	fake.runs++
	return fake.settings, nil
}

func TestAgentRetentionHandlerLifecycle(t *testing.T) {
	service := &fakeAgentRetention{settings: model.AgentRetentionSettings{
		RolloutDays: 14,
		Environment: model.AgentRetentionDefaults{RolloutDays: 30, Enabled: true},
		Status:      model.AgentRetentionRunStatus{LastStatus: model.AgentRetentionStatusOK},
	}}
	handler := NewAgentRetentionHandler(service)
	recorder := httptest.NewRecorder()
	handler.HandleGet(recorder, jsonRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("environment")) {
		t.Fatalf("get status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.HandleUpdate(recorder, jsonRequest(http.MethodPut, "/", bytes.NewBufferString(
		`{"rollout_days":7,"release_days":0}`)))
	if recorder.Code != http.StatusOK || len(service.updates) != 1 {
		t.Fatalf("update status = %d, updates = %+v", recorder.Code, service.updates)
	}
	if service.updates[0].RolloutDays != 7 || service.updates[0].ReleaseDays != 0 {
		t.Fatalf("update input = %+v", service.updates[0])
	}

	recorder = httptest.NewRecorder()
	handler.HandleReset(recorder, jsonRequest(http.MethodDelete, "/", nil))
	if recorder.Code != http.StatusOK || service.resets != 1 {
		t.Fatalf("reset status = %d, resets = %d", recorder.Code, service.resets)
	}

	recorder = httptest.NewRecorder()
	handler.HandleRun(recorder, jsonRequest(http.MethodPost, "/run", nil))
	if recorder.Code != http.StatusOK || service.runs != 1 {
		t.Fatalf("run status = %d, runs = %d", recorder.Code, service.runs)
	}
}

func TestAgentRetentionHandlerRejectsInvalidInput(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
	}{
		"broken json":      {"{", http.StatusBadRequest},
		"out of range":     {`{"rollout_days":4000}`, http.StatusBadRequest},
		"negative release": {`{"release_days":-1}`, http.StatusBadRequest},
		"unknown field":    {`{"rollout_days":1,"extra":true}`, http.StatusBadRequest},
	}
	for name, test := range cases {
		service := &fakeAgentRetention{updateErr: model.ErrAgentRetentionInvalid}
		recorder := httptest.NewRecorder()
		NewAgentRetentionHandler(service).HandleUpdate(recorder,
			jsonRequest(http.MethodPut, "/", bytes.NewBufferString(test.body)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want %d", name, recorder.Code, test.status)
		}
	}
}

func TestAgentRetentionHandlerErrorMapping(t *testing.T) {
	unavailable := errors.New("store is down")
	cases := map[string]struct {
		service *fakeAgentRetention
		call    func(*AgentRetentionHandler, *httptest.ResponseRecorder)
		status  int
	}{
		"get failure": {&fakeAgentRetention{getErr: unavailable},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleGet(w, jsonRequest(http.MethodGet, "/", nil))
			}, http.StatusServiceUnavailable},
		"update invalid": {&fakeAgentRetention{updateErr: model.ErrAgentRetentionInvalid},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleUpdate(w, jsonRequest(http.MethodPut, "/", bytes.NewBufferString(`{"rollout_days":4000}`)))
			}, http.StatusBadRequest},
		"update failure": {&fakeAgentRetention{updateErr: unavailable},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleUpdate(w, jsonRequest(http.MethodPut, "/", bytes.NewBufferString(`{"rollout_days":1}`)))
			}, http.StatusServiceUnavailable},
		"reset disabled": {&fakeAgentRetention{resetErr: retentionadmin.ErrDisabled},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleReset(w, jsonRequest(http.MethodDelete, "/", nil))
			}, http.StatusConflict},
		"reset failure": {&fakeAgentRetention{resetErr: unavailable},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleReset(w, jsonRequest(http.MethodDelete, "/", nil))
			}, http.StatusServiceUnavailable},
		"run disabled": {&fakeAgentRetention{runErr: retentionadmin.ErrDisabled},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleRun(w, jsonRequest(http.MethodPost, "/run", nil))
			}, http.StatusConflict},
		"run failure": {&fakeAgentRetention{runErr: unavailable},
			func(h *AgentRetentionHandler, w *httptest.ResponseRecorder) {
				h.HandleRun(w, jsonRequest(http.MethodPost, "/run", nil))
			}, http.StatusServiceUnavailable},
	}
	for name, test := range cases {
		recorder := httptest.NewRecorder()
		test.call(NewAgentRetentionHandler(test.service), recorder)
		if recorder.Code != test.status {
			t.Fatalf("%s status = %d, want %d", name, recorder.Code, test.status)
		}
	}
}
