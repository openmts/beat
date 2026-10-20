package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/fleetsummary"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type fakeFleetSummaries struct {
	schedules []model.FleetSummarySchedule
	revisions []int
	listErr   error
	createErr error
	updateErr error
	deleteErr error
	testErr   error
	run       model.FleetSummaryRun
}

func (f *fakeFleetSummaries) List(context.Context) ([]model.FleetSummarySchedule, error) {
	return f.schedules, f.listErr
}

func (f *fakeFleetSummaries) Create(
	_ context.Context,
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	schedule.ID, schedule.Revision = "created", 1
	f.schedules = append(f.schedules, *schedule)
	return schedule, nil
}

func (f *fakeFleetSummaries) Update(
	_ context.Context,
	id string,
	expectedRevision int,
	schedule *model.FleetSummarySchedule,
) (*model.FleetSummarySchedule, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.revisions = append(f.revisions, expectedRevision)
	schedule.ID, schedule.Revision = id, expectedRevision+1
	return schedule, nil
}

func (f *fakeFleetSummaries) Delete(_ context.Context, id string, expectedRevision int) error {
	f.revisions = append(f.revisions, expectedRevision)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if id == "missing" {
		return fleetsummary.ErrScheduleNotFound
	}
	return nil
}

func (f *fakeFleetSummaries) TestRun(context.Context, string) (model.FleetSummaryRun, error) {
	if f.testErr != nil {
		return model.FleetSummaryRun{}, f.testErr
	}
	f.run = model.FleetSummaryRun{ID: "run", Status: model.FleetSummaryResultQueued}
	return f.run, nil
}

func fleetSummaryAdminRequest(method, target string, body *bytes.Buffer) *http.Request {
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = jsonRequest(method, target, body)
		request.Header.Set("Content-Type", "application/json")
	}
	principal := model.AdminPrincipal{User: model.AdminUser{ID: "admin"}, Session: model.AdminSession{ID: "session"}}
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
}

func TestFleetSummaryHandlerLifecycle(t *testing.T) {
	service := &fakeFleetSummaries{}
	handler := NewFleetSummaryHandler(service)
	payload := `{"name":"Morning","cadence":"daily","timezone":"UTC","local_time":"08:00","weekday":null,` +
		`"name_threshold":10,"enabled":true,"node_ids":["node"],"group_ids":[],"channel_ids":["channel"]}`
	recorder := httptest.NewRecorder()
	handler.HandleCreate(recorder, fleetSummaryAdminRequest(http.MethodPost, "/", bytes.NewBufferString(payload)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.HandleList(recorder, jsonRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("Morning")) {
		t.Fatalf("list status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request := fleetSummaryAdminRequest(http.MethodPut, "/created",
		bytes.NewBufferString(payload[:len(payload)-1]+`,"expected_revision":3}`))
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleUpdate(recorder, request)
	if recorder.Code != http.StatusOK || len(service.revisions) != 1 || service.revisions[0] != 3 {
		t.Fatalf("update status = %d, revisions = %#v", recorder.Code, service.revisions)
	}

	request = fleetSummaryAdminRequest(http.MethodPost, "/created/test-run", nil)
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleTestRun(recorder, request)
	if recorder.Code != http.StatusAccepted || !bytes.Contains(recorder.Body.Bytes(), []byte(`"run_id":"run"`)) {
		t.Fatalf("test status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request = fleetSummaryAdminRequest(http.MethodDelete, "/created", bytes.NewBufferString(`{"expected_revision":4}`))
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleDelete(recorder, request)
	if recorder.Code != http.StatusNoContent || service.revisions[len(service.revisions)-1] != 4 {
		t.Fatalf("delete status = %d, revisions = %#v", recorder.Code, service.revisions)
	}

	request = httptest.NewRequest(http.MethodDelete, "/created", nil)
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleDelete(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("bodyless delete status = %d", recorder.Code)
	}
}

func TestFleetSummaryHandlerValidationAndAuthorization(t *testing.T) {
	handler := NewFleetSummaryHandler(&fakeFleetSummaries{})
	recorder := httptest.NewRecorder()
	handler.HandleCreate(recorder, jsonRequest(http.MethodPost, "/", bytes.NewBufferString("{")))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.HandleDelete(recorder, jsonRequest(http.MethodDelete, "/created", bytes.NewBufferString("{")))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid delete body status = %d", recorder.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/created/test-run", nil)
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleTestRun(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated test status = %d", recorder.Code)
	}
}

func TestFleetSummaryHandlerErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		service *fakeFleetSummaries
		call    func(*FleetSummaryHandler, *httptest.ResponseRecorder)
		status  int
	}{
		{"list failure", &fakeFleetSummaries{listErr: errors.New("boom")},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				h.HandleList(w, jsonRequest(http.MethodGet, "/", nil))
			}, http.StatusInternalServerError},
		{"invalid schedule", &fakeFleetSummaries{createErr: fleetsummary.ErrInvalidSchedule},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				h.HandleCreate(w, jsonRequest(http.MethodPost, "/", bytes.NewBufferString(`{}`)))
			}, http.StatusBadRequest},
		{"conflict", &fakeFleetSummaries{updateErr: fleetsummary.ErrConflict},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				request := jsonRequest(http.MethodPut, "/id", bytes.NewBufferString(`{}`))
				request.Header.Set("Content-Type", "application/json")
				request.SetPathValue("id", "id")
				h.HandleUpdate(w, request)
			}, http.StatusConflict},
		{"store conflict", &fakeFleetSummaries{deleteErr: store.ErrFleetSummaryConflict},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				request := httptest.NewRequest(http.MethodDelete, "/id", nil)
				request.SetPathValue("id", "id")
				h.HandleDelete(w, request)
			}, http.StatusConflict},
		{"too large", &fakeFleetSummaries{testErr: fleetsummary.ErrSummaryTooLarge},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				h.HandleTestRun(w, fleetSummaryAdminRequest(http.MethodPost, "/id/test-run", nil))
			}, http.StatusUnprocessableEntity},
		{"rate limited", &fakeFleetSummaries{testErr: notificationqueue.ErrRateLimited},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				h.HandleTestRun(w, fleetSummaryAdminRequest(http.MethodPost, "/id/test-run", nil))
			}, http.StatusTooManyRequests},
		{"queue unavailable", &fakeFleetSummaries{testErr: notificationqueue.ErrQueueUnavailable},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				h.HandleTestRun(w, fleetSummaryAdminRequest(http.MethodPost, "/id/test-run", nil))
			}, http.StatusServiceUnavailable},
		{"session expired", &fakeFleetSummaries{testErr: store.ErrAdminSessionNotFound},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				h.HandleTestRun(w, fleetSummaryAdminRequest(http.MethodPost, "/id/test-run", nil))
			}, http.StatusUnauthorized},
		{"missing schedule", &fakeFleetSummaries{},
			func(h *FleetSummaryHandler, w *httptest.ResponseRecorder) {
				request := httptest.NewRequest(http.MethodDelete, "/missing", nil)
				request.SetPathValue("id", "missing")
				h.HandleDelete(w, request)
			}, http.StatusNotFound},
	}
	for _, test := range cases {
		recorder := httptest.NewRecorder()
		test.call(NewFleetSummaryHandler(test.service), recorder)
		if recorder.Code != test.status {
			t.Fatalf("%s status = %d, want %d", test.name, recorder.Code, test.status)
		}
	}
}
