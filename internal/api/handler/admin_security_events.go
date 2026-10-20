package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/loginsecurity"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// AdminSecurityHandler 提供登录安全事件历史、确认与通知策略接口。
type AdminSecurityHandler struct{ service *adminauth.Service }

func NewAdminSecurityHandler(service *adminauth.Service) *AdminSecurityHandler {
	return &AdminSecurityHandler{service: service}
}

func (handler *AdminSecurityHandler) HandleListEvents(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	filter, err := securityEventFilter(r)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid security event filter")
		return
	}
	page, err := handler.service.ListSecurityEvents(r.Context(), &principal, filter)
	if err != nil {
		writeAdminSecurityError(w, err)
		return
	}
	for index := range page.Events {
		page.Events[index] = withSecuritySummaries(page.Events[index])
	}
	JSONResponse(w, http.StatusOK, page)
}

func (handler *AdminSecurityHandler) HandleEventDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	event, err := handler.service.SecurityEvent(r.Context(), &principal, r.PathValue("id"))
	if err != nil {
		writeAdminSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, withSecuritySummaries(event))
}

func (handler *AdminSecurityHandler) HandleAcknowledgeEvent(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := decodeAuthBody(w, r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid acknowledgement request")
		return
	}
	event, err := handler.service.AcknowledgeSecurityEvent(r.Context(), &principal, r.PathValue("id"), body.Note)
	if err != nil {
		writeAdminSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, withSecuritySummaries(event))
}

func (handler *AdminSecurityHandler) HandlePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	settings, err := handler.service.SecurityPolicy(r.Context(), &principal)
	if err != nil {
		writeAdminSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

func (handler *AdminSecurityHandler) HandleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var body model.SecurityNotificationSettingsWrite
	if err := decodeAuthBody(w, r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid security notification policy")
		return
	}
	settings, err := handler.service.UpdateSecurityPolicy(r.Context(), &principal, body)
	if err != nil {
		writeAdminSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

func (handler *AdminSecurityHandler) HandleEventSource(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	source, err := handler.service.SecurityEventSource(r.Context(), &principal, r.PathValue("id"))
	if err != nil {
		writeAdminSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, source)
}

// HandleOptions 返回固定选项词表；前端只渲染标签，不展示机器值。
func (handler *AdminSecurityHandler) HandleOptions(w http.ResponseWriter, _ *http.Request) {
	JSONResponse(w, http.StatusOK, securityOptions())
}

func withSecuritySummaries(event model.SecurityEvent) model.SecurityEvent {
	event.SourceSummary = loginsecurity.SummaryFromSource(event.SourceClass, event.SourcePrefix)
	event.DeviceSummary = loginsecurity.SummaryFromDevice(event.BrowserFamily, event.OSFamily, event.DeviceClass)
	return event
}

func securityEventFilter(r *http.Request) (model.SecurityEventFilter, error) {
	query := r.URL.Query()
	filter := model.SecurityEventFilter{Kind: query.Get("kind"), Severity: query.Get("severity"),
		ActorID: query.Get("actor"), SourceClass: query.Get("source"), RoutingState: query.Get("routing"),
		Reason: query.Get("reason"), DeliveryState: query.Get("delivery")}
	limit, err := securityLimit(query.Get("limit"))
	if err != nil {
		return model.SecurityEventFilter{}, err
	}
	offset, err := securityOffset(query.Get("offset"))
	if err != nil {
		return model.SecurityEventFilter{}, err
	}
	filter.Limit, filter.Offset = limit, offset
	return filter, nil
}

func securityLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 200 {
		return 0, errors.New("limit is out of range")
	}
	return value, nil
}

func securityOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, errors.New("offset is out of range")
	}
	return value, nil
}

func writeAdminSecurityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrSecurityEventNotFound):
		JSONError(w, http.StatusNotFound, "security event was not found")
	case errors.Is(err, store.ErrSecurityEventSourceUnavailable):
		JSONError(w, http.StatusConflict, "no protected source record for this event")
	case errors.Is(err, store.ErrSecurityEventAcknowledged):
		JSONError(w, http.StatusConflict, "security event is already acknowledged")
	case errors.Is(err, store.ErrSecuritySettingsRevision):
		JSONError(w, http.StatusConflict, "security policy changed; refresh and retry")
	case errors.Is(err, store.ErrSecuritySettingsInvalid), errors.Is(err, store.ErrSecurityFilterInvalid),
		errors.Is(err, store.ErrSecurityEventInvalid), errors.Is(err, store.ErrSecurityThresholdInvalid):
		JSONError(w, http.StatusBadRequest, "security request is invalid")
	case errors.Is(err, adminauth.ErrForbidden):
		JSONError(w, http.StatusForbidden, "owner permission is required")
	case errors.Is(err, adminauth.ErrRecentAuthRequired):
		JSONError(w, http.StatusPreconditionRequired, "recent authentication is required")
	default:
		JSONError(w, http.StatusServiceUnavailable, "security history is unavailable")
	}
}
