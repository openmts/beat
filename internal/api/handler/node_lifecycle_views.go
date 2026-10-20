package handler

import (
	"context"

	"github.com/beat/backend/internal/model"
)

type NodeLifecycleViews interface {
	PublicLifecycle(context.Context, string) (*model.LifecyclePublic, error)
}

func (h *NodeHandler) WithLifecycleViews(views NodeLifecycleViews) *NodeHandler {
	h.lifecycleViews = views
	return h
}

func (h *MetricsHandler) WithLifecycleViews(views NodeLifecycleViews) *MetricsHandler {
	h.lifecycleViews = views
	return h
}
