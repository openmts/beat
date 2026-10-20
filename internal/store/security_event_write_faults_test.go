package store

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type covResult struct {
	affected int64
	err      error
}

func (result covResult) LastInsertId() (int64, error) { return 0, nil }

func (result covResult) RowsAffected() (int64, error) { return result.affected, result.err }

func covValidFingerprint(actorID string, now time.Time) SecurityFingerprint {
	return SecurityFingerprint{ActorUserID: actorID, Kind: SecurityFingerprintKindSource, Epoch: "epoch-1",
		Fingerprint: bytes.Repeat([]byte{7}, 32), SeenAt: now.UnixMilli(),
		RetainUntil: now.AddDate(0, 0, 180).UnixMilli()}
}

// TestAdmitLoginSessionFailsClosedOnSessionCapFault 覆盖活跃会话裁剪写失败时，
// 会话、审计与派生数据一起回滚。
func TestAdmitLoginSessionFailsClosedOnSessionCapFault(t *testing.T) {
	store, admission := localAdmissionFixture(t, false)
	now, expiry := model.NowUTC(), model.NowUTC().Add(time.Hour)
	for index := range model.MaxActiveAdminSessions {
		seedSession(t, store, fmt.Sprintf("cov-cap-%02d", index),
			now.Add(-time.Duration(index+1)*time.Minute), expiry, nil)
	}
	installCustomMetricFault(t, store.db, `CREATE TRIGGER cov_session_cap
		BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(ABORT,'cap blocked'); END`)
	admission.Audit.CreatedAt = admission.Session.CreatedAt
	if err := store.AdmitLoginSession(t.Context(), LoginSessionAdmission{Session: admission}); err == nil {
		t.Fatal("blocked session cap write was accepted")
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM admin_sessions WHERE id=?", admission.Session.ID); got != 0 {
		t.Fatalf("failed admission left %d sessions", got)
	}
}

// TestAdmitLoginSessionRejectsInvalidSecurityEvents 覆盖安全事件写入前的
// 文本边界、词表校验与时间顺序校验。
func TestAdmitLoginSessionRejectsInvalidSecurityEvents(t *testing.T) {
	cases := map[string]func(*model.SecurityEvent){
		"missing timestamps": func(event *model.SecurityEvent) {
			event.OccurredAt, event.RetainUntil = time.Time{}, time.Time{}
		},
		"overlong source key":    func(event *model.SecurityEvent) { event.SourceKey = strings.Repeat("k", 600) },
		"unknown auth method":    func(event *model.SecurityEvent) { event.AuthMethod = "carrier-pigeon" },
		"unknown source class":   func(event *model.SecurityEvent) { event.SourceClass = "carrier" },
		"too many reasons":       func(event *model.SecurityEvent) { event.Reasons = make([]string, len(model.SecurityReasonCodes())+1) },
		"unknown reason":         func(event *model.SecurityEvent) { event.Reasons = []string{"not-a-reason"} },
		"retention before event": func(event *model.SecurityEvent) { event.RetainUntil = event.OccurredAt.Add(-time.Hour) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store, admission := localAdmissionFixture(t, false)
			event := loginAdmissionEvent("cov-event", admission.User.ID, model.NowUTC())
			mutate(&event)
			admission.Audit.CreatedAt = admission.Session.CreatedAt
			err := store.AdmitLoginSession(t.Context(), LoginSessionAdmission{
				Session: admission, Events: []model.SecurityEvent{event}})
			if !errors.Is(err, ErrSecurityEventInvalid) {
				t.Fatalf("invalid security event = %v", err)
			}
		})
	}
}

// TestAdmitLoginSessionRejectsInvalidFingerprints 覆盖指纹写入前的形状校验。
func TestAdmitLoginSessionRejectsInvalidFingerprints(t *testing.T) {
	store, admission := localAdmissionFixture(t, false)
	admission.Audit.CreatedAt = admission.Session.CreatedAt
	fingerprint := covValidFingerprint(admission.User.ID, model.NowUTC())
	fingerprint.Kind = "carrier"
	err := store.AdmitLoginSession(t.Context(), LoginSessionAdmission{
		Session: admission, Fingerprints: []SecurityFingerprint{fingerprint}})
	if !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("invalid fingerprint = %v", err)
	}
}

// TestAdmitLoginSessionFailsClosedOnFingerprintWriteFault 覆盖指纹表写入失败。
func TestAdmitLoginSessionFailsClosedOnFingerprintWriteFault(t *testing.T) {
	store, admission := localAdmissionFixture(t, false)
	installCustomMetricFault(t, store.db, `CREATE TRIGGER cov_fingerprint
		BEFORE INSERT ON security_login_fingerprints BEGIN SELECT RAISE(ABORT,'fingerprint blocked'); END`)
	admission.Audit.CreatedAt = admission.Session.CreatedAt
	fingerprint := covValidFingerprint(admission.User.ID, model.NowUTC())
	if err := store.AdmitLoginSession(t.Context(), LoginSessionAdmission{
		Session: admission, Fingerprints: []SecurityFingerprint{fingerprint}}); err == nil {
		t.Fatal("blocked fingerprint write was accepted")
	}
}

// TestSecurityFingerprintValidationEdges 覆盖指纹形状校验的全部分支。
func TestSecurityFingerprintValidationEdges(t *testing.T) {
	now := model.NowUTC()
	if !securityFingerprintValid(covValidFingerprint("user-1", now)) {
		t.Fatal("valid fingerprint was rejected")
	}
	cases := map[string]func(*SecurityFingerprint){
		"missing actor":         func(value *SecurityFingerprint) { value.ActorUserID = "" },
		"short fingerprint":     func(value *SecurityFingerprint) { value.Fingerprint = []byte("short") },
		"missing seen time":     func(value *SecurityFingerprint) { value.SeenAt = 0 },
		"retention before seen": func(value *SecurityFingerprint) { value.RetainUntil = value.SeenAt - 1 },
		"missing epoch":         func(value *SecurityFingerprint) { value.Epoch = "" },
		"overlong epoch":        func(value *SecurityFingerprint) { value.Epoch = strings.Repeat("e", 65) },
		"unknown kind":          func(value *SecurityFingerprint) { value.Kind = "carrier" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fingerprint := covValidFingerprint("user-1", now)
			mutate(&fingerprint)
			if securityFingerprintValid(fingerprint) {
				t.Fatal("invalid fingerprint was accepted")
			}
		})
	}
}

// TestSecurityRowsAffectedRejectsBrokenResults 覆盖受影响行数读取失败的失败关闭。
func TestSecurityRowsAffectedRejectsBrokenResults(t *testing.T) {
	if err := securityRowsAffected(covResult{affected: 1}, nil, "insert security event"); err != nil {
		t.Fatalf("healthy result = %v", err)
	}
	if err := securityRowsAffected(covResult{affected: 0}, nil, "insert security event"); !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("missing row = %v", err)
	}
	if err := securityRowsAffected(nil, errors.New("driver failure"), "insert security event"); !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("driver failure = %v", err)
	}
	broken := covResult{err: errors.New("rows affected unavailable")}
	if err := securityRowsAffected(broken, nil, "insert security event"); !errors.Is(err, ErrSecurityEventInvalid) {
		t.Fatalf("broken rows affected = %v", err)
	}
}
