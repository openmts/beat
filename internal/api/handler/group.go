package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type GroupHandler struct {
	groupStore *store.GroupStore
}

func NewGroupHandler(groupStore *store.GroupStore) *GroupHandler {
	return &GroupHandler{
		groupStore: groupStore,
	}
}

func (h *GroupHandler) HandleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.groupStore.ListGroups(r.Context())
	if err != nil {
		// 客户端取消与内部故障分开记：前者不是 5xx，也不该占 WARN。
		if clientCanceled(r.Context()) || errors.Is(err, context.Canceled) {
			respondClientCanceled(w, r, "list groups")

			return
		}
		// 500 只在响应里给固定文案，这里补一条可诊断的原因。
		slog.WarnContext(r.Context(), "list groups failed", "error", err)
		JSONError(w, http.StatusInternalServerError, "failed to list groups")

		return
	}

	JSONResponse(w, http.StatusOK, groups)
}

func (h *GroupHandler) HandleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)

		return
	}

	if body.Name == "" {
		JSONError(w, http.StatusBadRequest, "name is required")

		return
	}

	group, err := h.groupStore.CreateGroup(r.Context(), body.Name)
	if err != nil {
		if respondMetadataMutationError(w, err) {
			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to create group")

		return
	}

	JSONResponse(w, http.StatusCreated, group)
}

func (h *GroupHandler) HandleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Name string `json:"name"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)

		return
	}

	if body.Name == "" {
		JSONError(w, http.StatusBadRequest, "name is required")

		return
	}

	if err := h.groupStore.UpdateGroup(r.Context(), id, body.Name); err != nil {
		if respondMetadataMutationError(w, err) {
			return
		}
		if err.Error() == "store: group "+id+" not found" {
			JSONError(w, http.StatusNotFound, "group not found")

			return
		}

		JSONError(w, http.StatusInternalServerError, "failed to update group")

		return
	}

	JSONResponse(w, http.StatusOK, &model.Group{ID: id, Name: body.Name})
}

func (h *GroupHandler) HandleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.groupStore.DeleteGroup(r.Context(), id); err != nil {
		if respondMetadataMutationError(w, err) {
			return
		}
		if respondOperationLifecycleConflict(w, err) {
			return
		}
		if respondAlertScopeCapacity(w, err) {
			return
		}
		if err == store.ErrDefaultGroupDelete {
			JSONError(w, http.StatusForbidden, "cannot delete default group")

			return
		}

		JSONError(w, http.StatusInternalServerError, "failed to delete group")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *GroupHandler) HandleUpdateSortOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if err := h.groupStore.UpdateSortOrder(r.Context(), body.IDs); err != nil {
		if respondMetadataMutationError(w, err) {
			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to update sort order")

		return
	}

	JSONResponse(w, http.StatusOK, nil)
}

func (h *GroupHandler) HandleSetDefaultGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.groupStore.SetDefaultGroup(r.Context(), id); err != nil {
		if respondMetadataMutationError(w, err) {
			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to set default group")

		return
	}

	JSONResponse(w, http.StatusOK, nil)
}
