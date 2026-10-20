package handler

import "net/http"

type panelAlertRequest struct {
	RuleIDs []string `json:"rule_ids"`
}

func (handler *DashboardCollaborationHandler) HandleListPanelAlerts(w http.ResponseWriter, request *http.Request) {
	if _, ok := handler.actor(w, request); !ok {
		return
	}
	if handler.panelAlerts == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	values, err := handler.panelAlerts.List(request.Context(), request.PathValue("id"))
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, values)
}

func (handler *DashboardCollaborationHandler) HandleReplacePanelAlerts(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.panelAlerts == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	var body panelAlertRequest
	if err := decodeCollaborationBody(w, request, 4096, &body); err != nil {
		respondCollaborationError(w, err)
		return
	}
	ruleIDs := body.RuleIDs
	if ruleIDs == nil {
		ruleIDs = []string{}
	}
	if err := handler.panelAlerts.Replace(request.Context(), request.PathValue("id"), request.PathValue("panelId"), ruleIDs, actor); err != nil {
		respondCollaborationError(w, err)
		return
	}
	values, err := handler.panelAlerts.List(request.Context(), request.PathValue("id"))
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, values)
}
