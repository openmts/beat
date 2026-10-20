package handler

import (
	"context"

	"github.com/beat/backend/internal/model"
)

type NodeLocationViews interface {
	PublicLocation(context.Context, string) (*model.LocationPublic, error)
}

func (h *NodeHandler) WithLocationViews(views NodeLocationViews) *NodeHandler {
	h.locationViews = views
	return h
}

func (h *MetricsHandler) WithLocationViews(views NodeLocationViews) *MetricsHandler {
	h.locationViews = views
	return h
}
