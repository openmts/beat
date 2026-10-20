package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type annotationRequest struct {
	DashboardID string    `json:"dashboard_id"`
	PanelID     string    `json:"panel_id"`
	Title       string    `json:"title"`
	Text        string    `json:"text"`
	Tags        []string  `json:"tags"`
	Color       string    `json:"color"`
	Visibility  string    `json:"visibility"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
}

func (handler *DashboardCollaborationHandler) HandleListAnnotations(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	handler.listAnnotations(w, request, actor.UserID)
}

// HandlePublicAnnotations 只返回 public 可见性注释，供无登录的公开看板读取。
func (handler *DashboardCollaborationHandler) HandlePublicAnnotations(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	handler.listAnnotations(w, request, "")
}

func (handler *DashboardCollaborationHandler) listAnnotations(w http.ResponseWriter, request *http.Request, userID string) {
	if handler.annotations == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	dashboardID := strings.TrimSpace(request.URL.Query().Get("dashboard_id"))
	if dashboardID == "" || len(dashboardID) > 80 {
		respondCollaborationError(w, errCollaborationBadRequest)
		return
	}
	from, to, err := annotationWindow(request)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	values, err := handler.annotations.List(request.Context(), store.AnnotationFilter{DashboardID: dashboardID, From: from, To: to, UserID: userID})
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, values)
}

func annotationWindow(request *http.Request) (time.Time, time.Time, error) {
	now := model.NowUTC()
	from, to := now.Add(-7*24*time.Hour), now.Add(24*time.Hour)
	parsed := map[string]*time.Time{"from": &from, "to": &to}
	for key, target := range parsed {
		raw := strings.TrimSpace(request.URL.Query().Get(key))
		if raw == "" {
			continue
		}
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return from, to, errCollaborationBadRequest
		}
		*target = value.UTC()
	}
	if to.Before(from) {
		return from, to, errCollaborationBadRequest
	}
	return from, to, nil
}

func (handler *DashboardCollaborationHandler) HandleCreateAnnotation(w http.ResponseWriter, request *http.Request) {
	handler.writeAnnotation(w, request, false)
}

func (handler *DashboardCollaborationHandler) HandleUpdateAnnotation(w http.ResponseWriter, request *http.Request) {
	handler.writeAnnotation(w, request, true)
}

func (handler *DashboardCollaborationHandler) writeAnnotation(w http.ResponseWriter, request *http.Request, update bool) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.annotations == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	var body annotationRequest
	if err := decodeCollaborationBody(w, request, model.MaxAnnotationTextBytes+4096, &body); err != nil {
		respondCollaborationError(w, err)
		return
	}
	value := model.DashboardAnnotation{DashboardID: strings.TrimSpace(body.DashboardID), PanelID: body.PanelID,
		Title: strings.TrimSpace(body.Title), Text: body.Text, Tags: body.Tags, Color: body.Color,
		Visibility: body.Visibility, From: body.From.UTC(), To: body.To.UTC()}
	if err := handler.applyAnnotation(w, request, store.AnnotationWrite{Value: value, Actor: actor}, update); err != nil {
		respondCollaborationError(w, err)
		return
	}
}

func (handler *DashboardCollaborationHandler) applyAnnotation(w http.ResponseWriter, request *http.Request, write store.AnnotationWrite, update bool) error {
	if !update {
		value, err := handler.annotations.Create(request.Context(), write)
		if err != nil {
			return err
		}
		JSONResponse(w, http.StatusCreated, value)
		return nil
	}
	write.Value.ID = request.PathValue("id")
	value, err := handler.annotations.Update(request.Context(), write)
	if err != nil {
		return err
	}
	JSONResponse(w, http.StatusOK, value)
	return nil
}

func (handler *DashboardCollaborationHandler) HandleDeleteAnnotation(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.annotations == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	if err := handler.annotations.Delete(request.Context(), request.PathValue("id"), actor); err != nil {
		respondCollaborationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
