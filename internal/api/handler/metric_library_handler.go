package handler

import (
	"net/http"
	"strings"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type libraryPanelRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Panel       model.DashboardPanel `json:"panel"`
}

func (handler *DashboardCollaborationHandler) HandleListLibraryPanels(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.library == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	values, err := handler.library.List(request.Context(), actor.UserID)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, values)
}

func (handler *DashboardCollaborationHandler) HandleCreateLibraryPanel(w http.ResponseWriter, request *http.Request) {
	handler.writeLibraryPanel(w, request, false)
}

func (handler *DashboardCollaborationHandler) HandleUpdateLibraryPanel(w http.ResponseWriter, request *http.Request) {
	handler.writeLibraryPanel(w, request, true)
}

func (handler *DashboardCollaborationHandler) writeLibraryPanel(w http.ResponseWriter, request *http.Request, update bool) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.library == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	var body libraryPanelRequest
	if err := decodeCollaborationBody(w, request, model.MaxLibraryPanelBytes+4096, &body); err != nil {
		respondCollaborationError(w, err)
		return
	}
	write := store.LibraryPanelWrite{Actor: actor, Value: model.DashboardLibraryPanel{
		Name: strings.TrimSpace(body.Name), Description: body.Description, Panel: body.Panel}}
	var (
		value *model.DashboardLibraryPanel
		err   error
	)
	if update {
		write.Value.ID = request.PathValue("id")
		value, err = handler.library.Update(request.Context(), write)
	} else {
		value, err = handler.library.Create(request.Context(), write)
	}
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	status := http.StatusOK
	if !update {
		status = http.StatusCreated
	}
	JSONResponse(w, status, value)
}

func (handler *DashboardCollaborationHandler) HandleDeleteLibraryPanel(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.library == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	if err := handler.library.Delete(request.Context(), request.PathValue("id"), actor); err != nil {
		respondCollaborationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
