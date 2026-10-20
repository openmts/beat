package adminauth

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/loginsecurity"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

const (
	securitySourceKindEvent = "security_event"
	securityDisplayLimit    = 200
	securityReasonLimit     = 8
)

// securityEventCommon 汇总构造事件与通知负载时共享的上下文。
type securityEventCommon struct {
	input   loginSecurityInput
	source  loginsecurity.Source
	device  loginsecurity.Device
	now     time.Time
	privacy string
}

// loginEvents 始终记录成功事件，并在原因命中时追加可疑事件与出箱请求。
func (service *Service) loginEvents(
	settings model.SecurityNotificationSettings, input loginSecurityInput,
	source loginsecurity.Source, device loginsecurity.Device, reasons []string,
) loginProvenance {
	common := securityEventCommon{input: input, source: source, device: device, now: input.Now.UTC(), privacy: settings.PrivacyLevel}
	events := []model.SecurityEvent{service.successSecurityEvent(settings, common)}
	if len(reasons) > 0 {
		events = append(events, service.suspiciousSecurityEvent(settings, common, reasons))
	}
	provenance := loginProvenance{Events: events}
	for _, event := range events {
		if event.RoutingState != model.SecurityRoutingNotRouted {
			continue
		}
		provenance.Requests = append(provenance.Requests, service.securityRequest(common, event))
		provenance.QueuedEventIDs = append(provenance.QueuedEventIDs, event.ID)
	}
	return provenance
}

func (service *Service) successSecurityEvent(settings model.SecurityNotificationSettings, common securityEventCommon) model.SecurityEvent {
	session := common.input.Principal.Session
	return service.newSecurityEvent(settings, common, model.SecurityEvent{
		Kind: model.SecurityEventLoginSucceeded, Severity: model.SecuritySeverityInfo,
		SourceKey: "login-success:" + session.ID, SourceID: session.ID, OccurredAt: session.CreatedAt})
}

// suspiciousSecurityEvent 把成功登录中的新来源/新设备合并为一条聚合事件。
func (service *Service) suspiciousSecurityEvent(
	settings model.SecurityNotificationSettings, common securityEventCommon, reasons []string,
) model.SecurityEvent {
	session := common.input.Principal.Session
	return service.newSecurityEvent(settings, common, model.SecurityEvent{
		Kind: model.SecurityEventLoginSuspicious, Severity: securityReasonsSeverity(reasons),
		Reasons: boundReasons(reasons), SourceKey: "login-suspicious:" + session.ID,
		SourceID: session.ID, OccurredAt: session.CreatedAt})
}

// securityReasonsSeverity 采用有界规则：新来源叠加新设备记为高严重级。
func securityReasonsSeverity(reasons []string) string {
	if len(boundReasons(reasons)) > 1 {
		return model.SecuritySeverityHigh
	}
	return model.SecuritySeverityMedium
}

func boundReasons(reasons []string) []string {
	if len(reasons) > securityReasonLimit {
		return reasons[:securityReasonLimit]
	}
	return reasons
}

func (service *Service) newSecurityEvent(
	settings model.SecurityNotificationSettings, common securityEventCommon, event model.SecurityEvent,
) model.SecurityEvent {
	event.ID = uuid.NewString()
	event.ActorUserID = common.input.Principal.User.ID
	event.ActorDisplayName = displaySnapshot(common.input.Principal.User)
	event.AuthMethod = common.input.AuthMethod
	event.SourceClass, event.SourceFamily, event.SourcePrefix = common.source.Class, common.source.Family, common.source.Prefix
	event.BrowserFamily, event.OSFamily, event.DeviceClass = common.device.Browser, common.device.OS, common.device.Class
	event.PolicyRevision = settings.Revision
	event.SourceKind = securitySourceKindEvent
	event.AuditEventID = common.input.AuditID
	event.RoutingState = service.securityRouting(settings, event.Kind)
	event.RetainUntil = common.now.AddDate(0, 0, settings.RetentionDays)
	return event
}

// securityRouting 决定事件是否需要出箱：停用的事件类型标记为 skipped。
func (service *Service) securityRouting(settings model.SecurityNotificationSettings, kind string) string {
	if !kindEnabled(settings, kind) {
		return model.SecurityRoutingSkipped
	}
	if service.config.Queue == nil {
		return model.SecurityRoutingUnavailable
	}
	return model.SecurityRoutingNotRouted
}

func kindEnabled(settings model.SecurityNotificationSettings, kind string) bool {
	for _, enabled := range settings.EnabledEventKinds {
		if enabled == kind {
			return true
		}
	}
	return false
}

func displaySnapshot(user model.AdminUser) string {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Username
	}
	return truncateRunes(name, securityDisplayLimit)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (service *Service) securityRequest(common securityEventCommon, event model.SecurityEvent) notificationqueue.Request {
	return notificationqueue.Request{
		SourceKey: event.SourceKey, SourceKind: securitySourceKindEvent, SourceID: event.ID,
		EventKind: event.Kind, Subject: securitySubject(event), Text: securityText(event),
		Data: securityData(common, event), UseSubscriptions: true,
	}
}

func securitySubject(event model.SecurityEvent) string {
	if event.Kind == model.SecurityEventLoginSuspicious {
		return "Suspicious administrator sign-in detected"
	}
	return "Administrator sign-in succeeded"
}

func securityText(event model.SecurityEvent) string {
	reasons := "none"
	if len(event.Reasons) > 0 {
		reasons = strings.Join(event.Reasons, ", ")
	}
	return fmt.Sprintf("%s at %s; source %s; device %s; reasons %s",
		securitySubject(event), event.OccurredAt.UTC().Format(time.RFC3339),
		event.SourcePrefix, loginsecurity.SummaryFromDevice(event.BrowserFamily, event.OSFamily, event.DeviceClass), reasons)
}

// securityData 只输出固定键；完整来源地址仅在 owner 显式选择后进入负载。
func securityData(common securityEventCommon, event model.SecurityEvent) map[string]string {
	data := map[string]string{
		"event.kind":                     event.Kind,
		"event.severity":                 event.Severity,
		"event.occurred_at":              event.OccurredAt.UTC().Format(time.RFC3339),
		"security.authentication_method": event.AuthMethod,
		"security.source_summary":        sourceSummary(common, event),
		"security.device_summary":        loginsecurity.SummaryFromDevice(event.BrowserFamily, event.OSFamily, event.DeviceClass),
		"security.reason_summary":        strings.Join(event.Reasons, ", "),
		"security.administrator":         event.ActorDisplayName,
	}
	return data
}

func sourceSummary(common securityEventCommon, event model.SecurityEvent) string {
	if common.privacy == "full_source" && common.input.Principal.Session.IPAddress != "" {
		return common.input.Principal.Session.IPAddress
	}
	return loginsecurity.SummaryFromSource(event.SourceClass, event.SourcePrefix)
}
