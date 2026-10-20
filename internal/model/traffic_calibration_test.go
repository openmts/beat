package model

import (
	"strings"
	"testing"
	"time"
)

func calibrationUUID(index int) string {
	const alphabet = "0123456789abcdef"
	digits := make([]byte, 0, 32)
	for range 32 {
		digits = append(digits, alphabet[index%len(alphabet)])
		index /= len(alphabet)
	}
	return string(digits[0:8]) + "-" + string(digits[8:12]) + "-" + string(digits[12:16]) + "-" +
		string(digits[16:20]) + "-" + string(digits[20:32])
}

func calibrationSetEvent(t *testing.T, effective time.Time, sent, received int64) TrafficCalibrationEvent {
	t.Helper()
	node := "node-1"
	sentValue, receivedValue := sent, received
	instant := effective.UTC()
	event := TrafficCalibrationEvent{ID: calibrationUUID(1), NodeID: &node, NodeName: "node",
		Kind: TrafficCalibrationSet, EffectiveAt: &instant, TargetSent: &sentValue, TargetReceived: &receivedValue,
		Reason: "provider reported opening balance", CreatedAt: effective.Add(time.Minute).UTC(),
		Actor: TrafficCalibrationActor{ID: "user-1", Name: "owner", RequestID: calibrationUUID(2)}}
	if err := event.Validate(); err != nil {
		t.Fatalf("fixture event invalid: %v", err)
	}
	return event
}

func TestParseTrafficCalibrationBytes(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int64
		valid bool
	}{
		{"zero", "0", 0, true},
		{"maximum", "9007199254740992", 9007199254740992, true},
		{"small", "1048576", 1048576, true},
		{"empty", "", 0, false},
		{"negative", "-1", 0, false},
		{"plus", "+1", 0, false},
		{"leading zero", "01", 0, false},
		{"overflow", "9007199254740993", 0, false},
		{"whitespace", " 1", 0, false},
		{"float", "1.5", 0, false},
		{"hex", "0x10", 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseTrafficCalibrationBytes(testCase.value)
			if testCase.valid && err != nil {
				t.Fatalf("parse %q: %v", testCase.value, err)
			}
			if !testCase.valid && err == nil {
				t.Fatalf("parse %q should have failed", testCase.value)
			}
			if got != testCase.want {
				t.Fatalf("parse %q = %d, want %d", testCase.value, got, testCase.want)
			}
		})
	}
	if got := FormatTrafficCalibrationBytes(9007199254740992); got != "9007199254740992" {
		t.Fatalf("format = %q", got)
	}
}

func TestTrafficCalibrationEventValidateShapes(t *testing.T) {
	effective := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	base := calibrationSetEvent(t, effective, 10, 20)
	revoke := TrafficCalibrationEvent{ID: calibrationUUID(3), NodeName: "node", Kind: TrafficCalibrationRevoke,
		ReferenceEventID: func() *string { reference := base.ID; return &reference }(),
		Reason:           "revoked by administrator", CreatedAt: effective.Add(time.Hour),
		Actor: TrafficCalibrationActor{ID: "user-1", Name: "owner", RequestID: calibrationUUID(4)}}
	if err := revoke.Validate(); err != nil {
		t.Fatalf("revoke invalid: %v", err)
	}
	broken := []struct {
		name   string
		mutate func(*TrafficCalibrationEvent)
	}{
		{"unknown kind", func(e *TrafficCalibrationEvent) { e.Kind = "replace" }},
		{"missing effective", func(e *TrafficCalibrationEvent) { e.EffectiveAt = nil }},
		{"missing target", func(e *TrafficCalibrationEvent) { e.TargetSent = nil }},
		{"negative target", func(e *TrafficCalibrationEvent) { value := int64(-1); e.TargetSent = &value }},
		{"oversized target", func(e *TrafficCalibrationEvent) {
			value := int64(MaximumTrafficCalibrationTarget) + 1
			e.TargetReceived = &value
		}},
		{"set with reference", func(e *TrafficCalibrationEvent) { ref := e.ID; e.ReferenceEventID = &ref }},
		{"empty reason", func(e *TrafficCalibrationEvent) { e.Reason = "" }},
		{"long reason", func(e *TrafficCalibrationEvent) {
			e.Reason = strings.Repeat("x", MaximumTrafficCalibrationReasonBytes+1)
		}},
		{"null byte", func(e *TrafficCalibrationEvent) { e.Reason = "bad\x00reason" }},
		{"missing actor", func(e *TrafficCalibrationEvent) { e.Actor.ID = "" }},
		{"zero created", func(e *TrafficCalibrationEvent) { e.CreatedAt = time.Time{} }},
	}
	for _, testCase := range broken {
		t.Run(testCase.name, func(t *testing.T) {
			event := base
			testCase.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatalf("event %s should have failed validation", testCase.name)
			}
		})
	}
	revokeBroken := revoke
	revokeBroken.EffectiveAt = &effective
	if err := revokeBroken.Validate(); err == nil {
		t.Fatal("revoke with effective instant should have failed")
	}
	revokeBroken = revoke
	revokeBroken.ReferenceEventID = nil
	if err := revokeBroken.Validate(); err == nil {
		t.Fatal("revoke without reference should have failed")
	}
}

func TestSelectTrafficCalibrationPrefersNewestAnchor(t *testing.T) {
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	first := calibrationSetEvent(t, start.Add(time.Hour), 1, 1)
	second := calibrationSetEvent(t, start.Add(2*time.Hour), 2, 2)
	second.ID = calibrationUUID(9)
	second.Actor.RequestID = calibrationUUID(8)
	revoke := TrafficCalibrationEvent{ID: calibrationUUID(7), NodeName: "node", Kind: TrafficCalibrationRevoke,
		ReferenceEventID: &second.ID, Reason: "revoked", CreatedAt: start.Add(3 * time.Hour), Actor: second.Actor}
	revoke.Actor.RequestID = calibrationUUID(6)
	outside := calibrationSetEvent(t, end.Add(time.Hour), 5, 5)

	cases := []struct {
		name   string
		events []TrafficCalibrationEvent
		want   *TrafficCalibrationEvent
	}{
		{"empty", nil, nil},
		{"latest set wins", []TrafficCalibrationEvent{first, second}, &second},
		{"revoke wins", []TrafficCalibrationEvent{first, second, revoke}, &revoke},
		{"outside window ignored", []TrafficCalibrationEvent{outside}, nil},
		{"before window ignored", []TrafficCalibrationEvent{calibrationSetEvent(t, start.Add(-time.Hour), 3, 3)}, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := SelectTrafficCalibration(testCase.events, start, end)
			if testCase.want == nil {
				if got != nil {
					t.Fatalf("expected no event, got %#v", got)
				}
				return
			}
			if got == nil || got.ID != testCase.want.ID {
				t.Fatalf("selected = %#v, want %s", got, testCase.want.ID)
			}
		})
	}
}

func TestTrafficAccountingForModes(t *testing.T) {
	effective := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	set := calibrationSetEvent(t, effective, 10, 20)
	if accounting := TrafficAccountingFor(&set); accounting.Mode != TrafficAccountingCalibrated ||
		accounting.CalibratedAt == nil || !accounting.CalibratedAt.Equal(effective) {
		t.Fatalf("calibrated accounting = %#v", accounting)
	}
	revoke := TrafficCalibrationEvent{Kind: TrafficCalibrationRevoke, CreatedAt: effective}
	if accounting := TrafficAccountingFor(&revoke); accounting.Mode != TrafficAccountingRaw || accounting.CalibratedAt != nil {
		t.Fatalf("revoke accounting = %#v", accounting)
	}
	if accounting := TrafficAccountingFor(nil); accounting.Mode != TrafficAccountingRaw {
		t.Fatalf("nil accounting = %#v", accounting)
	}
}

func TestTrafficCalibrationRequestValidateAndCanonicalReason(t *testing.T) {
	request := TrafficCalibrationRequest{RequestID: calibrationUUID(5), TargetSent: 0, TargetReceived: 0,
		Reason: "  provider reconciliation  "}
	if err := request.Validate(); err != nil {
		t.Fatalf("request invalid: %v", err)
	}
	if got := request.CanonicalReason(); got != "provider reconciliation" {
		t.Fatalf("canonical reason = %q", got)
	}
	request.RequestID = "not-a-uuid"
	if err := request.Validate(); err == nil {
		t.Fatal("non-uuid request should have failed")
	}
	request.RequestID = calibrationUUID(1)
	request.Reason = "   "
	if err := request.Validate(); err == nil {
		t.Fatal("blank reason should have failed")
	}
	request.Reason = "ok"
	request.TargetSent = -1
	if err := request.Validate(); err == nil {
		t.Fatal("negative target should have failed")
	}
	request.TargetSent = 1
	zero := time.Time{}
	request.EffectiveAt = &zero
	if err := request.Validate(); err == nil {
		t.Fatal("zero effective instant should have failed")
	}
}

func TestTrafficCalibrationEventAnchor(t *testing.T) {
	effective := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	set := calibrationSetEvent(t, effective, 1, 1)
	if anchor := set.Anchor(); !anchor.Equal(effective) {
		t.Fatalf("set anchor = %v", anchor)
	}
	revoke := TrafficCalibrationEvent{Kind: TrafficCalibrationRevoke, CreatedAt: effective.Add(time.Hour)}
	if anchor := revoke.Anchor(); !anchor.Equal(effective.Add(time.Hour)) {
		t.Fatalf("revoke anchor = %v", anchor)
	}
	if !ValidTrafficCalibrationUUID(calibrationUUID(1)) || ValidTrafficCalibrationUUID("short") {
		t.Fatal("uuid validation mismatch")
	}
}
