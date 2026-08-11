package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/trafficreport"
)

type fakeTrafficReports struct {
	schedules []model.TrafficReportSchedule
	result    model.TrafficReportRunResult
	listErr   error
	createErr error
	updateErr error
	deleteErr error
	testErr   error
}

func (f *fakeTrafficReports) List(context.Context) ([]model.TrafficReportSchedule, error) {
	return f.schedules, f.listErr
}

func (f *fakeTrafficReports) Create(
	_ context.Context,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	schedule.ID = "created"
	f.schedules = append(f.schedules, *schedule)
	return schedule, nil
}

func (f *fakeTrafficReports) Update(
	_ context.Context,
	id string,
	schedule *model.TrafficReportSchedule,
) (*model.TrafficReportSchedule, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if id == "missing" {
		return nil, trafficreport.ErrScheduleNotFound
	}
	schedule.ID = id
	return schedule, nil
}

func (f *fakeTrafficReports) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if id == "missing" {
		return trafficreport.ErrScheduleNotFound
	}
	return nil
}

func (f *fakeTrafficReports) TestRun(
	_ context.Context,
	id string,
) (model.TrafficReportRunResult, error) {
	if f.testErr != nil {
		return f.result, f.testErr
	}
	if id == "missing" {
		return model.TrafficReportRunResult{}, trafficreport.ErrScheduleNotFound
	}
	return f.result, nil
}

func TestTrafficReportHandlerLifecycle(t *testing.T) {
	service := &fakeTrafficReports{result: model.TrafficReportRunResult{
		Delivery: model.TrafficReportDeliveryStatus{State: model.TrafficReportDeliverySuccess},
	}}
	handler := NewTrafficReportHandler(service)

	createBody := `{"name":"Daily","cadence":"daily","timezone":"UTC","send_hour":8,"all_nodes":true,"all_channels":true,"enabled":true}`
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(createBody))
	recorder := httptest.NewRecorder()
	handler.HandleCreate(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	recorder = httptest.NewRecorder()
	handler.HandleList(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("Daily")) {
		t.Fatalf("list status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/created/test", nil)
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleTestRun(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("success")) {
		t.Fatalf("test status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/created", nil)
	request.SetPathValue("id", "created")
	recorder = httptest.NewRecorder()
	handler.HandleDelete(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", recorder.Code)
	}
}

func TestTrafficReportHandlerValidatesRequestsAndMissingSchedules(t *testing.T) {
	handler := NewTrafficReportHandler(&fakeTrafficReports{})
	recorder := httptest.NewRecorder()
	handler.HandleCreate(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{")))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d", recorder.Code)
	}

	request := httptest.NewRequest(http.MethodPut, "/missing", bytes.NewBufferString(`{"name":"Daily"}`))
	request.SetPathValue("id", "missing")
	recorder = httptest.NewRecorder()
	handler.HandleUpdate(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/missing/test", nil)
	request.SetPathValue("id", "missing")
	recorder = httptest.NewRecorder()
	handler.HandleTestRun(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing test status = %d", recorder.Code)
	}
}

func TestTrafficReportRequestMapsExplicitScopes(t *testing.T) {
	var request trafficReportRequest
	payload := []byte(`{"name":"Weekly","cadence":"weekly","timezone":"Asia/Shanghai","send_hour":9,"send_minute":15,"weekday":5,"month_day":1,"all_nodes":false,"node_ids":["n1"],"all_channels":false,"channel_ids":["c1"],"enabled":true}`)
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	schedule := request.schedule()
	if schedule.Weekday != 5 || len(schedule.NodeIDs) != 1 || schedule.ChannelIDs[0] != "c1" {
		t.Fatalf("schedule = %#v", schedule)
	}
}

func TestTrafficReportHandlerServiceErrors(t *testing.T) {
	service := &fakeTrafficReports{listErr: errors.New("list failed")}
	handler := NewTrafficReportHandler(service)
	recorder := httptest.NewRecorder()
	handler.HandleList(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("list error status = %d", recorder.Code)
	}

	service.listErr = nil
	service.createErr = trafficreport.ErrInvalidSchedule
	recorder = httptest.NewRecorder()
	handler.HandleCreate(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"bad"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create validation status = %d", recorder.Code)
	}
	service.createErr = errors.New("create failed")
	recorder = httptest.NewRecorder()
	handler.HandleCreate(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"bad"}`)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("create error status = %d", recorder.Code)
	}

	service.deleteErr = trafficreport.ErrScheduleNotFound
	request := httptest.NewRequest(http.MethodDelete, "/missing", nil)
	request.SetPathValue("id", "missing")
	recorder = httptest.NewRecorder()
	handler.HandleDelete(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d", recorder.Code)
	}

	service.result.Delivery.State = model.TrafficReportDeliveryFailed
	service.testErr = errors.New("delivery failed")
	request = httptest.NewRequest(http.MethodPost, "/schedule/test", nil)
	request.SetPathValue("id", "schedule")
	recorder = httptest.NewRecorder()
	handler.HandleTestRun(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delivery failure status = %d", recorder.Code)
	}
}
