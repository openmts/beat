package adminauth

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/loginsecurity"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

// loginFailureInput 描述一次失败尝试的隐私受控输入。
type loginFailureInput struct {
	Principal *model.AdminPrincipal
	Username  string
	IPAddress string
	UserAgent string
	Reason    string
	Now       time.Time
}

// thresholdRule 绑定一个持久化阈值键与它对应的固定原因代码。
type thresholdRule struct {
	key    store.LoginThresholdKey
	reason string
	limit  int
}

// recordLoginFailure 持久化失败阈值与审计，必要时下发一次可疑登录事件。
func (service *Service) recordLoginFailure(ctx context.Context, input loginFailureInput) {
	settings, credentials, err := service.securitySettings(ctx)
	if err != nil {
		slog.Error("read login security settings", "error_code", "security_settings_unavailable")
		return
	}
	rules := service.thresholdRules(settings, credentials, input)
	if len(rules) == 0 {
		return
	}
	outcomes, err := service.config.Store.AdmitLoginFailure(ctx, loginFailureAdmission{
		service: service, input: input, settings: settings, rules: rules}.admission())
	if err != nil {
		slog.Error("record login failure", "error_code", "login_failure_unrecorded")
		return
	}
	service.emitSuspiciousThresholds(ctx, settings, input, rules, outcomes)
}

// thresholdRules 为来源与用户名分别建立计数键，并对无效 TOTP 追加独立阈值。
func (service *Service) thresholdRules(
	settings model.SecurityNotificationSettings, credentials store.SecurityCredentials, input loginFailureInput,
) []thresholdRule {
	source := loginsecurity.SummarizeSource(input.IPAddress)
	username := model.NormalizeUsername(input.Username)
	rules := []thresholdRule{
		{key: thresholdKey(credentials, store.SecurityFingerprintKindSource, "source:"+source.Prefix),
			reason: model.SecurityReasonRateLimitLockout, limit: settings.ThresholdFailures},
		{key: thresholdKey(credentials, "username", "password:"+username),
			reason: model.SecurityReasonRateLimitLockout, limit: settings.ThresholdFailures},
	}
	if input.Reason == model.SecurityReasonInvalidTOTP {
		rules = append(rules, thresholdRule{key: thresholdKey(credentials, "username", "totp:"+username),
			reason: model.SecurityReasonInvalidTOTP, limit: settings.InvalidTOTPThreshold})
	}
	return rules
}

func thresholdKey(credentials store.SecurityCredentials, kind, value string) store.LoginThresholdKey {
	return store.LoginThresholdKey{KeyKind: kind,
		KeyHash: loginsecurity.Fingerprint(credentials.FingerprintKey, credentials.FingerprintEpoch, value)}
}

// loginFailureAdmission 组装失败审计与阈值增量。
type loginFailureAdmission struct {
	service  *Service
	input    loginFailureInput
	settings model.SecurityNotificationSettings
	rules    []thresholdRule
}

func (admission loginFailureAdmission) admission() store.LoginFailureAdmission {
	window := time.Duration(admission.settings.ThresholdWindowSecond) * time.Second
	bumps := make([]store.ThresholdBump, 0, len(admission.rules))
	for _, rule := range admission.rules {
		bumps = append(bumps, store.ThresholdBump{KeyKind: rule.key.KeyKind, KeyHash: rule.key.KeyHash,
			Now: admission.input.Now, Window: window, Limit: rule.limit, Lockout: loginLockout})
	}
	return store.LoginFailureAdmission{Audit: admission.audit(), Thresholds: bumps}
}

func (admission loginFailureAdmission) audit() model.AdminAuditEvent {
	input := AuditInput{Principal: admission.input.Principal, Action: "auth.login", ResourceType: "session",
		Outcome: model.AuditOutcomeFailure, IPAddress: admission.input.IPAddress, UserAgent: admission.input.UserAgent}
	return admission.service.auditEvent(context.Background(), input)
}

// emitSuspiciousThresholds 只对跨过阈值且本窗口未下发的窗口创建事件。
func (service *Service) emitSuspiciousThresholds(
	ctx context.Context, settings model.SecurityNotificationSettings,
	input loginFailureInput, rules []thresholdRule, outcomes []store.ThresholdOutcome,
) {
	for index, outcome := range outcomes {
		if !outcome.ShouldEmit || index >= len(rules) {
			continue
		}
		event := service.thresholdSecurityEvent(settings, input, rules[index], outcome)
		switch event.RoutingState {
		case model.SecurityRoutingUnavailable:
			service.emitThresholdEventLocally(ctx, outcome, event, input.Now)
		case model.SecurityRoutingSkipped:
			continue
		default:
			service.emitThresholdEventQueued(ctx, outcome, event, input)
		}
	}
}

func (service *Service) emitThresholdEventLocally(
	ctx context.Context, outcome store.ThresholdOutcome, event model.SecurityEvent, at time.Time,
) {
	emitted, err := service.config.Store.EmitThresholdEvent(ctx, thresholdKeyOf(outcome), event, at)
	if err != nil || !emitted {
		service.observeSecurityOutboxFailure()
		slog.Error("emit login security event", "error_code", "security_event_unrecorded")
		return
	}
	service.observeSecurityEvents([]model.SecurityEvent{event})
}

func (service *Service) emitThresholdEventQueued(
	ctx context.Context, outcome store.ThresholdOutcome, event model.SecurityEvent, input loginFailureInput,
) {
	request := service.thresholdRequest(event, input)
	receipts, err := service.config.Queue.EnqueueBatch(ctx, []notificationqueue.Request{request}, func(tx *sql.Tx) error {
		marked, err := service.config.Store.MarkThresholdEmittedTx(ctx, tx, thresholdKeyOf(outcome), input.Now)
		if err != nil {
			return err
		}
		if !marked {
			return notificationqueue.ErrSkipMutation
		}
		return service.config.Store.InsertSecurityEventsTx(ctx, tx, []model.SecurityEvent{event})
	})
	if err != nil {
		service.observeSecurityOutboxFailure()
		slog.Error("emit login security notification", "error_code", "security_event_unrecorded")
		return
	}
	service.applySecurityReceipts(ctx, []string{event.ID}, receipts)
	service.observeSecurityEvents([]model.SecurityEvent{event})
}

func thresholdKeyOf(outcome store.ThresholdOutcome) store.ThresholdKey {
	return store.ThresholdKey{KeyKind: outcome.KeyKind, KeyHash: outcome.KeyHash, WindowAt: outcome.WindowAt}
}

// thresholdSecurityEvent 为阈值命中生成不含用户名的可疑事件。
func (service *Service) thresholdSecurityEvent(
	settings model.SecurityNotificationSettings, input loginFailureInput,
	rule thresholdRule, outcome store.ThresholdOutcome,
) model.SecurityEvent {
	source := loginsecurity.SummarizeSource(input.IPAddress)
	device := loginsecurity.SummarizeDevice(input.UserAgent)
	window := input.Now.UTC().Truncate(time.Second)
	event := model.SecurityEvent{
		ID: uuid.NewString(), Kind: model.SecurityEventLoginSuspicious,
		Severity: securityReasonSeverity(rule.reason), AuthMethod: model.SecurityAuthMethodUnknown,
		SourceClass: source.Class, SourceFamily: source.Family, SourcePrefix: source.Prefix,
		BrowserFamily: device.Browser, OSFamily: device.OS, DeviceClass: device.Class,
		Reasons: []string{rule.reason}, PolicyRevision: settings.Revision,
		SourceKey:  lockoutSourceKey(rule, outcome, window),
		SourceKind: securitySourceKindEvent, SourceID: outcome.KeyKind + ":" + hex.EncodeToString(outcome.KeyHash[:8]),
		RoutingState: service.securityRouting(settings, model.SecurityEventLoginSuspicious),
		OccurredAt:   input.Now.UTC(),
		RetainUntil:  input.Now.UTC().AddDate(0, 0, settings.RetentionDays),
	}
	if input.Principal != nil {
		event.ActorUserID = input.Principal.User.ID
		event.ActorDisplayName = displaySnapshot(input.Principal.User)
		event.AuthMethod = model.SecurityAuthMethodPassword
	}
	return event
}

// securityReasonSeverity 与设计一致：锁定为高严重级，其余阈值命中为中。
func securityReasonSeverity(reason string) string {
	if reason == model.SecurityReasonRateLimitLockout {
		return model.SecuritySeverityHigh
	}
	return model.SecuritySeverityMedium
}

func lockoutSourceKey(rule thresholdRule, outcome store.ThresholdOutcome, window time.Time) string {
	return fmt.Sprintf("login-suspicious:%s:%s:%d", rule.key.KeyKind,
		hex.EncodeToString(outcome.KeyHash[:8]), window.UnixMilli())
}

func (service *Service) thresholdRequest(event model.SecurityEvent, input loginFailureInput) notificationqueue.Request {
	common := securityEventCommon{input: loginSecurityInput{Principal: principalOrEmpty(input.Principal),
		AuthMethod: event.AuthMethod, AuditID: event.AuditEventID, Now: input.Now},
		source: loginsecurity.SummarizeSource(input.IPAddress), device: loginsecurity.SummarizeDevice(input.UserAgent),
		now: input.Now, privacy: ""}
	return service.securityRequest(common, event)
}

func principalOrEmpty(principal *model.AdminPrincipal) model.AdminPrincipal {
	if principal == nil {
		return model.AdminPrincipal{Session: model.AdminSession{IPAddress: ""}}
	}
	return *principal
}
