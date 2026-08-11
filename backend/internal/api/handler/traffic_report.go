package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/trafficreport"
)

type TrafficReportOperations interface {
	List(context.Context) ([]model.TrafficReportSchedule, error)
	Create(context.Context, *model.TrafficReportSchedule) (*model.TrafficReportSchedule, error)
	Update(context.Context, string, *model.TrafficReportSchedule) (*model.TrafficReportSchedule, error)
	Delete(context.Context, string) error
	TestRun(context.Context, string) (model.TrafficReportRunResult, error)
}

type TrafficReportHandler struct {
	service TrafficReportOperations
}

type trafficReportRequest struct {
	Name        string   `json:"name"`
	Cadence     string   `json:"cadence"`
	Timezone    string   `json:"timezone"`
	SendHour    int      `json:"send_hour"`
	SendMinute  int      `json:"send_minute"`
	Weekday     int      `json:"weekday"`
	MonthDay    int      `json:"month_day"`
	AllNodes    bool     `json:"all_nodes"`
	NodeIDs     []string `json:"node_ids"`
	AllChannels bool     `json:"all_channels"`
	ChannelIDs  []string `json:"channel_ids"`
	Enabled     bool     `json:"enabled"`
}

func NewTrafficReportHandler(service TrafficReportOperations) *TrafficReportHandler {
	return &TrafficReportHandler{service: service}
}

func (h *TrafficReportHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.service.List(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list traffic report schedules")
		return
	}
	JSONResponse(w, http.StatusOK, schedules)
}

func (h *TrafficReportHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	body, ok := parseTrafficReportRequest(w, r)
	if !ok {
		return
	}
	schedule := body.schedule()
	created, err := h.service.Create(r.Context(), &schedule)
	if err != nil {
		writeTrafficReportError(w, err, "failed to create traffic report schedule")
		return
	}
	JSONResponse(w, http.StatusCreated, created)
}

func (h *TrafficReportHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	body, ok := parseTrafficReportRequest(w, r)
	if !ok {
		return
	}
	schedule := body.schedule()
	updated, err := h.service.Update(r.Context(), r.PathValue("id"), &schedule)
	if err != nil {
		writeTrafficReportError(w, err, "failed to update traffic report schedule")
		return
	}
	JSONResponse(w, http.StatusOK, updated)
}

func (h *TrafficReportHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeTrafficReportError(w, err, "failed to delete traffic report schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TrafficReportHandler) HandleTestRun(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.TestRun(r.Context(), r.PathValue("id"))
	if err != nil && result.Delivery.State == "" {
		writeTrafficReportError(w, err, "failed to test traffic report schedule")
		return
	}
	JSONResponse(w, http.StatusOK, result)
}

func parseTrafficReportRequest(
	w http.ResponseWriter,
	r *http.Request,
) (trafficReportRequest, bool) {
	var body trafficReportRequest
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return body, false
	}
	return body, true
}

func (request trafficReportRequest) schedule() model.TrafficReportSchedule {
	return model.TrafficReportSchedule{
		Name: request.Name, Cadence: request.Cadence, Timezone: request.Timezone,
		SendHour: request.SendHour, SendMinute: request.SendMinute,
		Weekday: request.Weekday, MonthDay: request.MonthDay,
		AllNodes: request.AllNodes, NodeIDs: request.NodeIDs,
		AllChannels: request.AllChannels, ChannelIDs: request.ChannelIDs,
		Enabled: request.Enabled,
	}
}

func writeTrafficReportError(w http.ResponseWriter, err error, internalMessage string) {
	switch {
	case errors.Is(err, trafficreport.ErrInvalidSchedule):
		JSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, trafficreport.ErrScheduleNotFound):
		JSONError(w, http.StatusNotFound, trafficreport.ErrScheduleNotFound.Error())
	default:
		JSONError(w, http.StatusInternalServerError, internalMessage)
	}
}
