package handler

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/beat/backend/internal/fleetsummary"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

// FleetSummaryOperations 是舰队状态摘要的管理员操作集合。
type FleetSummaryOperations interface {
	List(context.Context) ([]model.FleetSummarySchedule, error)
	Create(context.Context, *model.FleetSummarySchedule) (*model.FleetSummarySchedule, error)
	Update(context.Context, string, int, *model.FleetSummarySchedule) (*model.FleetSummarySchedule, error)
	Delete(context.Context, string, int) error
	TestRun(context.Context, string) (model.FleetSummaryRun, error)
}

type FleetSummaryHandler struct {
	service FleetSummaryOperations
}

type fleetSummaryWriteRequest struct {
	Name             string   `json:"name"`
	Cadence          string   `json:"cadence"`
	Timezone         string   `json:"timezone"`
	LocalTime        string   `json:"local_time"`
	Weekday          *int     `json:"weekday"`
	NameThreshold    int      `json:"name_threshold"`
	Enabled          bool     `json:"enabled"`
	NodeIDs          []string `json:"node_ids"`
	GroupIDs         []string `json:"group_ids"`
	ChannelIDs       []string `json:"channel_ids"`
	ExpectedRevision int      `json:"expected_revision"`
}

type fleetSummaryRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

func NewFleetSummaryHandler(service FleetSummaryOperations) *FleetSummaryHandler {
	return &FleetSummaryHandler{service: service}
}

func (h *FleetSummaryHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.service.List(r.Context())
	if err != nil {
		writeFleetSummaryError(w, err, "failed to list fleet summary schedules")
		return
	}
	JSONResponse(w, http.StatusOK, schedules)
}

func (h *FleetSummaryHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	body, ok := parseFleetSummaryWrite(w, r)
	if !ok {
		return
	}
	schedule := body.schedule()
	created, err := h.service.Create(r.Context(), &schedule)
	if err != nil {
		writeFleetSummaryError(w, err, "failed to create fleet summary schedule")
		return
	}
	JSONResponse(w, http.StatusCreated, created)
}

func (h *FleetSummaryHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	body, ok := parseFleetSummaryWrite(w, r)
	if !ok {
		return
	}
	schedule := body.schedule()
	updated, err := h.service.Update(r.Context(), r.PathValue("id"), body.ExpectedRevision, &schedule)
	if err != nil {
		writeFleetSummaryError(w, err, "failed to update fleet summary schedule")
		return
	}
	JSONResponse(w, http.StatusOK, updated)
}

func (h *FleetSummaryHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	var body fleetSummaryWriteRequest
	// DELETE 允许携带 expected_revision 做乐观并发；空请求体按“使用当前版本”处理。
	if err := ParseJSONStrict(r, &body); err != nil && !errors.Is(err, io.EOF) {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.Delete(r.Context(), r.PathValue("id"), body.ExpectedRevision); err != nil {
		writeFleetSummaryError(w, err, "failed to delete fleet summary schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FleetSummaryHandler) HandleTestRun(w http.ResponseWriter, r *http.Request) {
	actor, ok := alertEventActor(w, r)
	if !ok {
		return
	}
	run, err := h.service.TestRun(fleetsummary.WithActor(r.Context(), actor), r.PathValue("id"))
	if err != nil {
		writeFleetSummaryError(w, err, "failed to test fleet summary schedule")
		return
	}
	JSONResponse(w, http.StatusAccepted, fleetSummaryRunResponse{RunID: run.ID, Status: run.Status})
}

func parseFleetSummaryWrite(w http.ResponseWriter, r *http.Request) (fleetSummaryWriteRequest, bool) {
	var body fleetSummaryWriteRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return body, false
	}
	return body, true
}

func (request fleetSummaryWriteRequest) schedule() model.FleetSummarySchedule {
	return model.FleetSummarySchedule{
		Name: request.Name, Cadence: request.Cadence, Timezone: request.Timezone,
		LocalTime: request.LocalTime, Weekday: request.Weekday, NameThreshold: request.NameThreshold,
		Enabled: request.Enabled, NodeIDs: request.NodeIDs, GroupIDs: request.GroupIDs,
		ChannelIDs: request.ChannelIDs,
	}
}

func writeFleetSummaryError(w http.ResponseWriter, err error, internalMessage string) {
	switch {
	case errors.Is(err, fleetsummary.ErrInvalidSchedule):
		JSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, fleetsummary.ErrScheduleNotFound), errors.Is(err, store.ErrFleetSummaryNotFound):
		JSONError(w, http.StatusNotFound, fleetsummary.ErrScheduleNotFound.Error())
	case errors.Is(err, fleetsummary.ErrConflict), errors.Is(err, store.ErrFleetSummaryConflict):
		JSONError(w, http.StatusConflict, fleetsummary.ErrConflict.Error())
	case errors.Is(err, fleetsummary.ErrSummaryTooLarge):
		JSONError(w, http.StatusUnprocessableEntity, fleetsummary.ErrSummaryTooLarge.Error())
	case errors.Is(err, notificationqueue.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		JSONError(w, http.StatusTooManyRequests, "wait before testing this summary again")
	case errors.Is(err, notificationqueue.ErrForbidden), errors.Is(err, store.ErrAdminSessionNotFound):
		JSONError(w, http.StatusUnauthorized, "administrator session is no longer active")
	case errors.Is(err, notificationqueue.ErrQueueUnavailable), errors.Is(err, notificationqueue.ErrQueueCapacity):
		JSONError(w, http.StatusServiceUnavailable, "notification queue unavailable; retry later")
	default:
		JSONError(w, http.StatusInternalServerError, internalMessage)
	}
}
