package handler

import (
	"time"

	"github.com/beat/backend/internal/model"
)

// trafficCalibrationView 是管理端事件视图：字节目标保持规范十进制字符串，
// 避免浏览器把 8 PiB 级别的整数按浮点截断。
type trafficCalibrationView struct {
	ID               string                        `json:"id"`
	NodeID           *string                       `json:"node_id"`
	NodeName         string                        `json:"node_name"`
	Kind             string                        `json:"kind"`
	EffectiveAt      *time.Time                    `json:"effective_at,omitempty"`
	TargetSent       *string                       `json:"target_sent,omitempty"`
	TargetReceived   *string                       `json:"target_received,omitempty"`
	Reason           string                        `json:"reason"`
	Actor            model.TrafficCalibrationActor `json:"actor"`
	ReferenceEventID *string                       `json:"reference_event_id,omitempty"`
	CreatedAt        time.Time                     `json:"created_at"`
}

func newTrafficCalibrationView(event model.TrafficCalibrationEvent) trafficCalibrationView {
	view := trafficCalibrationView{
		ID: event.ID, NodeID: event.NodeID, NodeName: event.NodeName, Kind: event.Kind,
		EffectiveAt: event.EffectiveAt, Reason: event.Reason, Actor: event.Actor,
		ReferenceEventID: event.ReferenceEventID, CreatedAt: event.CreatedAt,
	}
	if event.TargetSent != nil {
		formatted := model.FormatTrafficCalibrationBytes(*event.TargetSent)
		view.TargetSent = &formatted
	}
	if event.TargetReceived != nil {
		formatted := model.FormatTrafficCalibrationBytes(*event.TargetReceived)
		view.TargetReceived = &formatted
	}
	return view
}

func newTrafficCalibrationViews(events []model.TrafficCalibrationEvent) []trafficCalibrationView {
	views := make([]trafficCalibrationView, 0, len(events))
	for index := range events {
		views = append(views, newTrafficCalibrationView(events[index]))
	}
	return views
}
