package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// 校准事件分页的硬上限；游标是上一次返回的最后一行事件 UUID。
const (
	DefaultTrafficCalibrationPage = 25
	MaximumTrafficCalibrationRows = 200
	defaultRevokeReason           = "revoked by administrator"
)

// TrafficCalibrationStore 是追加式流量校准账本的唯一读写入口。
type TrafficCalibrationStore struct {
	db *sql.DB
}

func NewTrafficCalibrationStore(db *sql.DB) *TrafficCalibrationStore {
	return &TrafficCalibrationStore{db: db}
}

// TrafficCalibrationWindow 描述一次结算解析使用的周期区间（半开区间）。
type TrafficCalibrationWindow struct {
	NodeID string
	Start  time.Time
	End    time.Time
}

// TrafficCalibrationSetWrite 是一次 set 追加；EffectiveAt 由调用方在持锁后确定。
type TrafficCalibrationSetWrite struct {
	Request     model.TrafficCalibrationRequest
	NodeID      string
	NodeName    string
	EffectiveAt time.Time
	Actor       model.TrafficCalibrationActor
	Now         time.Time
}

// TrafficCalibrationRevokeWrite 撤销一个当前生效的 set 事件。
type TrafficCalibrationRevokeWrite struct {
	RequestID string
	NodeName  string
	Window    TrafficCalibrationWindow
	EventID   string
	Actor     model.TrafficCalibrationActor
	Now       time.Time
}

// TrafficCalibrationReplaceWrite 原子地追加一条 revoke 与一条替换 set。
type TrafficCalibrationReplaceWrite struct {
	Request     model.TrafficCalibrationRequest
	NodeName    string
	EffectiveAt time.Time
	Window      TrafficCalibrationWindow
	EventID     string
	Actor       model.TrafficCalibrationActor
	Now         time.Time
}

// TrafficCalibrationChange 是一次追加的结果；Replayed 表示命中了幂等重放。
type TrafficCalibrationChange struct {
	Event    model.TrafficCalibrationEvent
	Revoked  *model.TrafficCalibrationEvent
	Replayed bool
}

// AppendSet 追加一条 set 事件；相同 request_id 的重放返回原事件。
func (store *TrafficCalibrationStore) AppendSet(
	ctx context.Context,
	write TrafficCalibrationSetWrite,
) (TrafficCalibrationChange, error) {
	event, err := trafficCalibrationSetEvent(write)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	return store.append(ctx, event)
}

// AppendRevoke 追加一条 revoke 事件；只允许撤销当前周期正在生效的 set。
func (store *TrafficCalibrationStore) AppendRevoke(
	ctx context.Context,
	write TrafficCalibrationRevokeWrite,
) (TrafficCalibrationChange, error) {
	if !model.ValidTrafficCalibrationUUID(write.RequestID) || write.EventID == "" || write.Now.IsZero() {
		return TrafficCalibrationChange{}, model.ErrTrafficCalibrationInvalid
	}
	nodeID := write.Window.NodeID
	event := model.TrafficCalibrationEvent{ID: uuid.NewString(), NodeID: &nodeID, NodeName: write.NodeName,
		Kind: model.TrafficCalibrationRevoke, ReferenceEventID: &write.EventID,
		Reason: defaultRevokeReason, CreatedAt: write.Now,
		Actor: model.TrafficCalibrationActor{ID: write.Actor.ID, Name: write.Actor.Name,
			SessionID: write.Actor.SessionID, RequestID: write.RequestID}}
	return store.appendRevokeAndSet(ctx, write.Window, event, nil)
}

// AppendReplace 原子地撤销当前生效事件并追加替换事件。
func (store *TrafficCalibrationStore) AppendReplace(
	ctx context.Context,
	write TrafficCalibrationReplaceWrite,
) (TrafficCalibrationChange, error) {
	replacement, err := trafficCalibrationSetEvent(TrafficCalibrationSetWrite{Request: write.Request,
		NodeID: write.Window.NodeID, NodeName: write.NodeName, EffectiveAt: write.EffectiveAt,
		Actor: write.Actor, Now: write.Now})
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	replaceNodeID := write.Window.NodeID
	revoke := model.TrafficCalibrationEvent{ID: uuid.NewString(), NodeID: &replaceNodeID, NodeName: write.NodeName,
		Kind: model.TrafficCalibrationRevoke, ReferenceEventID: &write.EventID,
		Reason: defaultRevokeReason, CreatedAt: write.Now,
		Actor: model.TrafficCalibrationActor{ID: write.Actor.ID, Name: write.Actor.Name,
			SessionID: write.Actor.SessionID, RequestID: uuid.NewString()}}
	return store.appendRevokeAndSet(ctx, write.Window, revoke, &replacement)
}

func trafficCalibrationSetEvent(write TrafficCalibrationSetWrite) (model.TrafficCalibrationEvent, error) {
	if err := write.Request.Validate(); err != nil {
		return model.TrafficCalibrationEvent{}, err
	}
	if write.NodeID == "" || write.EffectiveAt.IsZero() || write.Now.IsZero() {
		return model.TrafficCalibrationEvent{}, model.ErrTrafficCalibrationInvalid
	}
	sent, received := write.Request.TargetSent, write.Request.TargetReceived
	effective, nodeID := write.EffectiveAt.UTC(), write.NodeID
	event := model.TrafficCalibrationEvent{ID: uuid.NewString(), NodeID: &nodeID, NodeName: write.NodeName,
		Kind: model.TrafficCalibrationSet, EffectiveAt: &effective, TargetSent: &sent, TargetReceived: &received,
		Reason: write.Request.CanonicalReason(), CreatedAt: write.Now,
		Actor: model.TrafficCalibrationActor{ID: write.Actor.ID, Name: write.Actor.Name,
			SessionID: write.Actor.SessionID, RequestID: write.Request.RequestID}}
	if err := event.Validate(); err != nil {
		return model.TrafficCalibrationEvent{}, err
	}
	return event, nil
}

func (store *TrafficCalibrationStore) append(
	ctx context.Context,
	event model.TrafficCalibrationEvent,
) (TrafficCalibrationChange, error) {
	if err := event.Validate(); err != nil {
		return TrafficCalibrationChange{}, err
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	replay, found, err := loadTrafficCalibrationReplay(ctx, tx, event)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	if found {
		if err := tx.Commit(); err != nil {
			return TrafficCalibrationChange{}, err
		}
		return TrafficCalibrationChange{Event: *replay, Replayed: true}, nil
	}
	if err := insertTrafficCalibration(ctx, tx, event); err != nil {
		return TrafficCalibrationChange{}, err
	}
	if err := mutation.finish(trafficCalibrationEvidence(event)); err != nil {
		return TrafficCalibrationChange{}, err
	}
	if err := tx.Commit(); err != nil {
		return TrafficCalibrationChange{}, err
	}
	return TrafficCalibrationChange{Event: event}, nil
}

func (store *TrafficCalibrationStore) appendRevokeAndSet(
	ctx context.Context,
	window TrafficCalibrationWindow,
	revoke model.TrafficCalibrationEvent,
	replacement *model.TrafficCalibrationEvent,
) (TrafficCalibrationChange, error) {
	if err := validateTrafficCalibrationWindow(window); err != nil {
		return TrafficCalibrationChange{}, err
	}
	if err := revoke.Validate(); err != nil {
		return TrafficCalibrationChange{}, err
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	primary := revoke
	if replacement != nil {
		primary = *replacement
	}
	replay, found, err := loadTrafficCalibrationReplay(ctx, tx, primary)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	if found {
		applied, err := findTrafficCalibrationRevoke(ctx, tx, *revoke.ReferenceEventID)
		if err != nil {
			return TrafficCalibrationChange{}, err
		}
		if err := tx.Commit(); err != nil {
			return TrafficCalibrationChange{}, err
		}
		return TrafficCalibrationChange{Event: *replay, Revoked: applied, Replayed: true}, nil
	}
	active, err := latestTrafficCalibration(ctx, tx, window)
	if err != nil {
		return TrafficCalibrationChange{}, err
	}
	if active == nil || active.ID != *revoke.ReferenceEventID || !active.Calibrated() {
		return TrafficCalibrationChange{}, model.ErrTrafficCalibrationConflict
	}
	if err := insertTrafficCalibration(ctx, tx, revoke); err != nil {
		return TrafficCalibrationChange{}, err
	}
	if replacement != nil {
		if err := insertTrafficCalibration(ctx, tx, *replacement); err != nil {
			return TrafficCalibrationChange{}, err
		}
	}
	if err := mutation.finish(trafficCalibrationEvidence(primary)); err != nil {
		return TrafficCalibrationChange{}, err
	}
	if err := tx.Commit(); err != nil {
		return TrafficCalibrationChange{}, err
	}
	return TrafficCalibrationChange{Event: primary, Revoked: &revoke}, nil
}

func validateTrafficCalibrationWindow(window TrafficCalibrationWindow) error {
	if window.NodeID == "" || window.Start.IsZero() || window.End.IsZero() || !window.Start.Before(window.End) {
		return model.ErrTrafficCalibrationInvalid
	}
	return nil
}

// EncodeTrafficCalibrationCursor 输出不透明游标，内部只是事件 UUID。
func EncodeTrafficCalibrationCursor(eventID string) string {
	if eventID == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(eventID))
}

// DecodeTrafficCalibrationCursor 解析游标；非法输入返回空串。
func DecodeTrafficCalibrationCursor(cursor string) string {
	if cursor == "" || len(cursor) > 64 {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !model.ValidTrafficCalibrationUUID(string(decoded)) {
		return ""
	}
	return string(decoded)
}
