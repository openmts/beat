package adminauth

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/beat/backend/internal/loginsecurity"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

// loginFingerprintRetentionDays 与 store 的指纹保留策略保持一致。
const loginFingerprintRetentionDays = 90

// loginSecurityInput 汇总一次已证明身份的登录所需的隐私受控输入。
type loginSecurityInput struct {
	Principal  model.AdminPrincipal
	AuthMethod string
	AuditID    string
	Now        time.Time
}

// loginProvenance 是一次会话建立需要原子写入的安全派生数据。
type loginProvenance struct {
	Events         []model.SecurityEvent
	Fingerprints   []store.SecurityFingerprint
	Requests       []notificationqueue.Request
	QueuedEventIDs []string
}

// loginSecurity 归一化来源与设备、计算可疑原因，并生成事件与通知请求。
func (service *Service) loginSecurity(ctx context.Context, input loginSecurityInput) (loginProvenance, error) {
	settings, credentials, err := service.securitySettings(ctx)
	if err != nil {
		return loginProvenance{}, err
	}
	source := loginsecurity.SummarizeSource(input.Principal.Session.IPAddress)
	device := loginsecurity.SummarizeDevice(input.Principal.Session.UserAgent)
	reasons, fingerprints, err := service.loginReasons(ctx, settings, credentials, input, source, device)
	if err != nil {
		return loginProvenance{}, err
	}
	provenance := service.loginEvents(settings, input, source, device, reasons)
	provenance.Fingerprints = fingerprints
	return provenance, nil
}

func (service *Service) securitySettings(ctx context.Context) (model.SecurityNotificationSettings, store.SecurityCredentials, error) {
	settings, err := service.config.Store.SecurityNotificationSettings(ctx)
	if err != nil {
		return model.SecurityNotificationSettings{}, store.SecurityCredentials{}, err
	}
	credentials, err := service.config.Store.SecurityCredentials(ctx)
	if err != nil {
		return model.SecurityNotificationSettings{}, store.SecurityCredentials{}, err
	}
	return settings, credentials, nil
}

// loginReasons 比较当前来源/设备指纹，返回固定原因代码与需要记录的指纹。
func (service *Service) loginReasons(
	ctx context.Context, settings model.SecurityNotificationSettings, credentials store.SecurityCredentials,
	input loginSecurityInput, source loginsecurity.Source, device loginsecurity.Device,
) ([]string, []store.SecurityFingerprint, error) {
	actorID := input.Principal.User.ID
	now := input.Now.UTC()
	retainUntil := now.AddDate(0, 0, loginFingerprintRetentionDays)
	values := []struct {
		kind  string
		value string
	}{
		{store.SecurityFingerprintKindSource, source.Prefix},
		{store.SecurityFingerprintKindDevice, deviceFingerprintValue(device)},
	}
	reasons := []string{}
	fingerprints := make([]store.SecurityFingerprint, 0, len(values))
	for _, value := range values {
		fingerprint := loginsecurity.Fingerprint(credentials.FingerprintKey, credentials.FingerprintEpoch, value.value)
		known, err := service.config.Store.KnownSecurityFingerprint(ctx, actorID, credentials.FingerprintEpoch,
			value.kind, fingerprint, now.AddDate(0, 0, -settings.LookbackDays).UnixMilli())
		if err != nil {
			return nil, nil, err
		}
		if !known {
			reasons = append(reasons, reasonForFingerprintKind(value.kind))
		}
		fingerprints = append(fingerprints, store.SecurityFingerprint{ActorUserID: actorID, Kind: value.kind,
			Epoch: credentials.FingerprintEpoch, Fingerprint: fingerprint,
			SeenAt: now.UnixMilli(), RetainUntil: retainUntil.UnixMilli()})
	}
	return reasons, fingerprints, nil
}

func reasonForFingerprintKind(kind string) string {
	if kind == store.SecurityFingerprintKindDevice {
		return model.SecurityReasonNewDeviceFamily
	}
	return model.SecurityReasonNewSourcePrefix
}

func deviceFingerprintValue(device loginsecurity.Device) string {
	return device.Browser + "/" + device.OS + "/" + device.Class
}

// commitLoginSecurity 让会话、审计、安全事件与通知出箱在同一事务内提交。
func (service *Service) commitLoginSecurity(
	ctx context.Context, admission store.LoginSessionAdmission, provenance loginProvenance,
) error {
	if service.config.Queue == nil || len(provenance.Requests) == 0 {
		if err := service.config.Store.AdmitLoginSession(ctx, admission); err != nil {
			service.observeSecurityTransactionFailure()
			return err
		}
		service.observeSecurityEvents(admission.Events)
		return nil
	}
	receipts, err := service.config.Queue.EnqueueBatch(ctx, provenance.Requests, func(tx *sql.Tx) error {
		return service.config.Store.AdmitLoginSessionTx(ctx, tx, admission)
	})
	if err != nil {
		service.observeSecurityTransactionFailure()
		return err
	}
	service.applySecurityReceipts(ctx, provenance.QueuedEventIDs, receipts)
	service.observeSecurityEvents(admission.Events)
	return nil
}

// applySecurityReceipts 记录队列回执确认的路由状态；失败只影响展示元数据。
func (service *Service) applySecurityReceipts(
	ctx context.Context, eventIDs []string, receipts []notificationqueue.Receipt,
) {
	for index, eventID := range eventIDs {
		if index >= len(receipts) {
			return
		}
		routing := receipts[index].RoutingState
		if err := service.config.Store.MarkSecurityEventRouting(ctx, eventID, routing); err != nil {
			slog.Warn("record security event routing", "error", err)
		}
	}
}
