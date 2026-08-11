package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type nodeUpdateRequest struct {
	Alias            string    `json:"alias"`
	GroupID          string    `json:"group_id"`
	SSHPublicKey     string    `json:"ssh_public_key"`
	TrafficLimit     *int64    `json:"traffic_limit"`
	TrafficLimitType *string   `json:"traffic_limit_type"`
	TrafficResetDay  *int      `json:"traffic_reset_day"`
	SortOrder        *int      `json:"sort_order"`
	Tags             *[]string `json:"tags"`
	IsPublic         *bool     `json:"is_public"`
	PublicRemark     *string   `json:"public_remark"`
	PrivateRemark    *string   `json:"private_remark"`
}

func (body *nodeUpdateRequest) normalizePresentation() error {
	if body.Tags != nil {
		tags, err := model.NormalizeNodeTags(*body.Tags)
		if err != nil {
			return err
		}
		body.Tags = &tags
	}
	trimOptionalString(body.PublicRemark)
	trimOptionalString(body.PrivateRemark)
	sortOrder := 0
	if body.SortOrder != nil {
		sortOrder = *body.SortOrder
	}
	return model.ValidateNodePresentation(
		sortOrder, optionalString(body.PublicRemark), optionalString(body.PrivateRemark),
	)
}

func (body nodeUpdateRequest) storeUpdate() store.NodeUpdate {
	return store.NodeUpdate{
		Alias: body.Alias, GroupID: body.GroupID, SSHPublicKey: body.SSHPublicKey,
		TrafficLimit: body.TrafficLimit, TrafficLimitType: body.TrafficLimitType,
		TrafficResetDay: body.TrafficResetDay, SortOrder: body.SortOrder, Tags: body.Tags,
		IsPublic: body.IsPublic, PublicRemark: body.PublicRemark, PrivateRemark: body.PrivateRemark,
	}
}

func (h *NodeHandler) HandleSortNodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := ParseJSON(r, &body); err != nil || len(body.IDs) == 0 {
		JSONError(w, http.StatusBadRequest, "invalid node sort order")
		return
	}
	if err := h.nodeStore.UpdateNodeSort(r.Context(), body.IDs); err != nil {
		if errors.Is(err, store.ErrInvalidNodeSort) {
			JSONError(w, http.StatusBadRequest, "invalid node sort order")
			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to update node sort order")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func trimOptionalString(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
