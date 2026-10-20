package model

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// 校准事件只有两个终态：set 声明一个生效秒的账目目标；revoke 让该周期回到原始 MTS。
const (
	TrafficCalibrationSet    = "set"
	TrafficCalibrationRevoke = "revoke"
)

// 公共与管理端只暴露这两个结算模式，绝不下发目标值、差值或事件历史。
const (
	TrafficAccountingRaw        = "raw"
	TrafficAccountingCalibrated = "calibrated"
)

// 校准目标与说明文字的硬边界；8 PiB 是单方向可接受的最大账面值。
const (
	MinimumTrafficCalibrationReasonBytes = 1
	MaximumTrafficCalibrationReasonBytes = 500
	MaximumTrafficCalibrationTarget      = int64(8) << 50
	MaximumTrafficCalibrationPage        = 100
	MaximumTrafficCalibrationActorBytes  = 64
	MaximumTrafficCalibrationNodeBytes   = 128
)

var (
	ErrTrafficCalibrationInvalid  = errors.New("invalid traffic calibration")
	ErrTrafficCalibrationConflict = errors.New("traffic calibration conflict")
	ErrTrafficCalibrationMissing  = errors.New("traffic calibration not found")
)

// TrafficCalibrationActor 是操作者快照；它随事件一起保存，账号改名后历史仍然可读。
type TrafficCalibrationActor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
}

// TrafficCalibrationEvent 是追加式账本里的一行。TargetSent/TargetReceived 只在 set 事件出现。
type TrafficCalibrationEvent struct {
	ID               string     `json:"id"`
	NodeID           *string    `json:"node_id"`
	NodeName         string     `json:"node_name"`
	Kind             string     `json:"kind"`
	EffectiveAt      *time.Time `json:"effective_at"`
	TargetSent       *int64     `json:"target_sent"`
	TargetReceived   *int64     `json:"target_received"`
	Reason           string     `json:"reason"`
	Actor            TrafficCalibrationActor
	ReferenceEventID *string   `json:"reference_event_id"`
	CreatedAt        time.Time `json:"created_at"`
}

// Anchor 是事件参与周期归属与排序的时间点：set 用生效秒，revoke 用创建时间。
func (event TrafficCalibrationEvent) Anchor() time.Time {
	if event.EffectiveAt != nil {
		return event.EffectiveAt.UTC()
	}
	return event.CreatedAt.UTC()
}

// Calibrated 报告该事件是否会把账目切换成校准模式。
func (event TrafficCalibrationEvent) Calibrated() bool {
	return event.Kind == TrafficCalibrationSet && event.EffectiveAt != nil &&
		event.TargetSent != nil && event.TargetReceived != nil
}

// Validate 只接受符合 kind 的事件形状，并拒绝越界目标与超长文本。
func (event TrafficCalibrationEvent) Validate() error {
	if !validTrafficCalibrationID(event.ID) || !validTrafficCalibrationID(event.Actor.RequestID) {
		return ErrTrafficCalibrationInvalid
	}
	if err := validateCalibrationAttribution(event); err != nil {
		return err
	}
	switch event.Kind {
	case TrafficCalibrationSet:
		return validateCalibrationSet(event)
	case TrafficCalibrationRevoke:
		if event.EffectiveAt != nil || event.TargetSent != nil || event.TargetReceived != nil {
			return ErrTrafficCalibrationInvalid
		}
		if event.ReferenceEventID == nil || !validTrafficCalibrationID(*event.ReferenceEventID) {
			return ErrTrafficCalibrationInvalid
		}
		return nil
	default:
		return ErrTrafficCalibrationInvalid
	}
}

func validateCalibrationSet(event TrafficCalibrationEvent) error {
	if event.EffectiveAt == nil || event.EffectiveAt.IsZero() {
		return ErrTrafficCalibrationInvalid
	}
	if event.TargetSent == nil || event.TargetReceived == nil {
		return ErrTrafficCalibrationInvalid
	}
	if err := validateCalibrationTarget(*event.TargetSent); err != nil {
		return err
	}
	if err := validateCalibrationTarget(*event.TargetReceived); err != nil {
		return err
	}
	if event.ReferenceEventID != nil {
		return ErrTrafficCalibrationInvalid
	}
	return nil
}

func validateCalibrationAttribution(event TrafficCalibrationEvent) error {
	if event.NodeName == "" || len(event.NodeName) > MaximumTrafficCalibrationNodeBytes {
		return ErrTrafficCalibrationInvalid
	}
	if event.Actor.ID == "" || len(event.Actor.ID) > MaximumTrafficCalibrationActorBytes {
		return ErrTrafficCalibrationInvalid
	}
	if event.Actor.Name == "" || len(event.Actor.Name) > MaximumTrafficCalibrationActorBytes {
		return ErrTrafficCalibrationInvalid
	}
	if len(event.Actor.SessionID) > MaximumTrafficCalibrationActorBytes {
		return ErrTrafficCalibrationInvalid
	}
	if len(event.Reason) < MinimumTrafficCalibrationReasonBytes || len(event.Reason) > MaximumTrafficCalibrationReasonBytes {
		return ErrTrafficCalibrationInvalid
	}
	if strings.ContainsRune(event.Reason, 0) || event.CreatedAt.IsZero() {
		return ErrTrafficCalibrationInvalid
	}
	return nil
}

func validateCalibrationTarget(value int64) error {
	if value < 0 || value > MaximumTrafficCalibrationTarget {
		return ErrTrafficCalibrationInvalid
	}
	return nil
}

func validTrafficCalibrationID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

// ValidTrafficCalibrationUUID 判断值是否为 36 字符的连字符 UUID 形状。
func ValidTrafficCalibrationUUID(value string) bool { return validTrafficCalibrationID(value) }

// ValidUUIDShape 判断值是否为 36 字符的连字符 UUID 形状；供其它账本复用同一契约。
func ValidUUIDShape(value string) bool { return validTrafficCalibrationID(value) }

// ParseTrafficCalibrationBytes 只接受规范十进制字符串，避免 JavaScript 整数截断。
func ParseTrafficCalibrationBytes(value string) (int64, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return 0, ErrTrafficCalibrationInvalid
	}
	if value != "0" && strings.HasPrefix(value, "0") {
		return 0, ErrTrafficCalibrationInvalid
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, ErrTrafficCalibrationInvalid
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrTrafficCalibrationInvalid
	}
	if err := validateCalibrationTarget(parsed); err != nil {
		return 0, err
	}
	return parsed, nil
}

// FormatTrafficCalibrationBytes 输出规范的十进制字符串。
func FormatTrafficCalibrationBytes(value int64) string { return strconv.FormatInt(value, 10) }

// SelectTrafficCalibration 在给定周期内挑出最新状态；没有事件时返回 nil。
func SelectTrafficCalibration(events []TrafficCalibrationEvent, start, end time.Time) *TrafficCalibrationEvent {
	var latest *TrafficCalibrationEvent
	for index := range events {
		candidate := &events[index]
		anchor := candidate.Anchor()
		if anchor.Before(start) || !anchor.Before(end) {
			continue
		}
		if latest == nil || trafficCalibrationNewer(candidate, latest) {
			latest = candidate
		}
	}
	return latest
}

func trafficCalibrationNewer(candidate, current *TrafficCalibrationEvent) bool {
	candidateAnchor, currentAnchor := candidate.Anchor(), current.Anchor()
	if !candidateAnchor.Equal(currentAnchor) {
		return candidateAnchor.After(currentAnchor)
	}
	if !candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.CreatedAt.After(current.CreatedAt)
	}
	return candidate.ID > current.ID
}

// TrafficAccountingFor 把最新状态翻译成公共响应的两个展示字段。
func TrafficAccountingFor(event *TrafficCalibrationEvent) TrafficAccounting {
	if event == nil || !event.Calibrated() {
		return TrafficAccounting{Mode: TrafficAccountingRaw}
	}
	effective := event.EffectiveAt.UTC()
	return TrafficAccounting{Mode: TrafficAccountingCalibrated, CalibratedAt: &effective}
}

// TrafficAccounting 是公共与管理端唯一可见的结算元数据。
type TrafficAccounting struct {
	Mode         string     `json:"accounting_mode"`
	CalibratedAt *time.Time `json:"calibrated_at,omitempty"`
}

// TrafficCalibrationRequest 是创建 set 事件的输入；字节值在解析阶段就已经是有界整数。
type TrafficCalibrationRequest struct {
	RequestID      string
	EffectiveAt    *time.Time
	TargetSent     int64
	TargetReceived int64
	Reason         string
}

// Validate 校验调用方传入的目标、原因与请求标识。
func (request TrafficCalibrationRequest) Validate() error {
	if !validTrafficCalibrationID(request.RequestID) {
		return ErrTrafficCalibrationInvalid
	}
	if err := validateCalibrationTarget(request.TargetSent); err != nil {
		return err
	}
	if err := validateCalibrationTarget(request.TargetReceived); err != nil {
		return err
	}
	reason := strings.TrimSpace(request.Reason)
	if len(reason) < MinimumTrafficCalibrationReasonBytes || len(reason) > MaximumTrafficCalibrationReasonBytes ||
		strings.ContainsRune(request.Reason, 0) {
		return ErrTrafficCalibrationInvalid
	}
	if request.EffectiveAt != nil && request.EffectiveAt.IsZero() {
		return ErrTrafficCalibrationInvalid
	}
	return nil
}

// CanonicalReason 返回裁剪后的说明文字，用于持久化。
func (request TrafficCalibrationRequest) CanonicalReason() string {
	return strings.TrimSpace(request.Reason)
}
