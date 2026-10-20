package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// ErrSecurityEventInvalid 表示安全事件字段不在固定词表或长度边界内。
var ErrSecurityEventInvalid = errors.New("store: security event is invalid")

// SecurityFingerprint 记录成功登录使用过的来源或设备指纹。
type SecurityFingerprint struct {
	ActorUserID string
	Kind        string
	Epoch       string
	Fingerprint []byte
	SeenAt      int64
	RetainUntil int64
}

// LoginSessionAdmission 在调用方事务内提交会话、审计与派生安全数据。
type LoginSessionAdmission struct {
	Session      LocalSessionAdmission
	Events       []model.SecurityEvent
	Fingerprints []SecurityFingerprint
}

// AdmitLoginSessionTx 供通知队列事务回调使用：会话、审计、安全事件、指纹同一事务提交。
func (store *AdminStore) AdmitLoginSessionTx(ctx context.Context, tx *sql.Tx, admission LoginSessionAdmission) error {
	return store.admitLoginSessionTx(ctx, tx, admission)
}

// AdmitLoginSession 在自有事务内提交相同内容，用于未启用通知队列的场景。
func (store *AdminStore) AdmitLoginSession(ctx context.Context, admission LoginSessionAdmission) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := store.admitLoginSessionTx(ctx, tx, admission); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *AdminStore) admitLoginSessionTx(ctx context.Context, tx *sql.Tx, admission LoginSessionAdmission) error {
	local := admission.Session
	if local.Bootstrap {
		store.bootstrapMu.Lock()
		defer store.bootstrapMu.Unlock()
	}
	if local.Session.AuthenticationMethod == "" {
		local.Session.AuthenticationMethod = model.AuthMethodLocal
	}
	if err := validateLocalAdmission(local); err != nil {
		return err
	}
	if local.Bootstrap {
		if err := insertBootstrapUser(ctx, tx, local.User); err != nil {
			return err
		}
	}
	if err := checkLoginUser(ctx, tx, local.User); err != nil {
		return err
	}
	if err := insertLocalSession(ctx, tx, local.Session); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE admin_users SET last_login_at=? WHERE id=?", local.Session.CreatedAt, local.User.ID)
	if err := sessionRowsAffected(result, err, "update login time"); err != nil {
		return err
	}
	if err := checkLocalAdmissionResult(ctx, tx, local); err != nil {
		return err
	}
	if err := store.enforceActiveSessionCap(ctx, tx, local); err != nil {
		return err
	}
	if err := insertAuditEvent(ctx, tx, &local.Audit); err != nil {
		return err
	}
	if err := insertSecurityEvents(ctx, tx, admission.Events); err != nil {
		return err
	}
	return upsertSecurityFingerprints(ctx, tx, admission.Fingerprints)
}

// enforceActiveSessionCap 在同一登录事务内把每用户活跃会话裁到上限，吊销最旧的多余会话。
// 计数只统计仍然活跃的会话（未吊销且未过期），过期但未吊销的会话不占用配额。
func (store *AdminStore) enforceActiveSessionCap(
	ctx context.Context,
	tx *sql.Tx,
	local LocalSessionAdmission,
) error {
	now := local.Session.CreatedAt
	result, err := tx.ExecContext(ctx, `UPDATE admin_sessions SET revoked_at = ? WHERE id IN (
		SELECT id FROM admin_sessions WHERE user_id = ? AND revoked_at IS NULL
			AND idle_expires_at > ? AND absolute_expires_at > ?
		ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?)`,
		now, local.User.ID, now, now, model.MaxActiveAdminSessions)
	if err != nil {
		return fmt.Errorf("enforce active session cap: %w", err)
	}
	revoked, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("enforce active session cap rows affected: %w", err)
	}
	if revoked == 0 {
		return nil
	}
	detail, err := json.Marshal(struct {
		Revoked int64 `json:"revoked"`
		Cap     int   `json:"cap"`
	}{Revoked: revoked, Cap: model.MaxActiveAdminSessions})
	if err != nil {
		return fmt.Errorf("encode session cap audit detail: %w", err)
	}
	event := model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: local.Audit.RequestID, ActorID: local.User.ID,
		ActorUsername: local.User.Username, Action: "session.cap_revoke", ResourceType: "administrator",
		ResourceID: local.User.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail),
		IPAddress: local.Audit.IPAddress, UserAgent: local.Audit.UserAgent,
		SessionPrefix: local.Audit.SessionPrefix, CreatedAt: now,
	}
	return insertAuditEvent(ctx, tx, &event)
}

// InsertSecurityEventsTx 供通知队列事务回调写入安全事件。
func (store *AdminStore) InsertSecurityEventsTx(ctx context.Context, tx *sql.Tx, events []model.SecurityEvent) error {
	return insertSecurityEvents(ctx, tx, events)
}

func insertSecurityEvents(ctx context.Context, tx *sql.Tx, events []model.SecurityEvent) error {
	for _, event := range events {
		if err := insertSecurityEvent(ctx, tx, event); err != nil {
			return err
		}
	}
	return nil
}

func insertSecurityEvent(ctx context.Context, tx *sql.Tx, event model.SecurityEvent) error {
	if err := validateSecurityEvent(event); err != nil {
		return err
	}
	reasons, err := json.Marshal(event.Reasons)
	if err != nil || !json.Valid(reasons) {
		return ErrSecurityEventInvalid
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO security_events (
		id,kind,severity,actor_user_id,actor_display_name,auth_method,source_class,source_family,source_prefix,
		browser_family,os_family,device_class,country_code,region,reasons_json,policy_revision,source_key,source_kind,
		source_id,message_id,routing_state,audit_event_id,occurred_at,created_at,retain_until
	) VALUES (?,?,?,NULLIF(?,''),?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULLIF(?,''),?,?,?,?,?)`,
		event.ID, event.Kind, event.Severity, event.ActorUserID, event.ActorDisplayName, event.AuthMethod,
		event.SourceClass, event.SourceFamily, event.SourcePrefix, event.BrowserFamily, event.OSFamily,
		event.DeviceClass, event.CountryCode, event.Region, string(reasons), event.PolicyRevision, event.SourceKey,
		event.SourceKind, event.SourceID, event.MessageID, event.RoutingState, event.AuditEventID,
		event.OccurredAt.UnixMilli(), event.OccurredAt.UnixMilli(), event.RetainUntil.UnixMilli())
	return securityRowsAffected(result, err, "insert security event")
}

func validateSecurityEvent(event model.SecurityEvent) error {
	if securityEventTextInvalid(event) || !securityEventVocabularyValid(event) {
		return ErrSecurityEventInvalid
	}
	if event.RetainUntil.Before(event.OccurredAt) || event.PolicyRevision < 0 {
		return ErrSecurityEventInvalid
	}
	return nil
}

func securityEventTextInvalid(event model.SecurityEvent) bool {
	fields := []struct {
		value string
		limit int
	}{
		{event.ID, 128}, {event.SourceKey, 512}, {event.ActorDisplayName, 200}, {event.SourcePrefix, 128},
		{event.BrowserFamily, 64}, {event.OSFamily, 64}, {event.DeviceClass, 32},
		{event.CountryCode, 8}, {event.Region, 128}, {event.SourceKind, 64}, {event.SourceID, 128},
		{event.AuditEventID, 128}, {event.MessageID, 128},
	}
	if event.OccurredAt.IsZero() || event.RetainUntil.IsZero() {
		return true
	}
	for _, field := range fields {
		if len(field.value) > field.limit {
			return true
		}
	}
	return event.ID == "" || event.SourceKey == ""
}

func securityRoutingValueValid(routing string) bool {
	return slices.Contains(securityRoutingValues, routing)
}

func securityEventVocabularyValid(event model.SecurityEvent) bool {
	if !slices.Contains(securityEventKindValues, event.Kind) || !slices.Contains(securitySeverityValues, event.Severity) {
		return false
	}
	if !slices.Contains(securityAuthMethodValues, event.AuthMethod) || !slices.Contains(securityRoutingValues, event.RoutingState) {
		return false
	}
	if !slices.Contains(securitySourceClassValues, event.SourceClass) || !slices.Contains(securitySourceFamilyValues, event.SourceFamily) {
		return false
	}
	return securityReasonsValid(event.Reasons)
}

func securityReasonsValid(reasons []string) bool {
	if len(reasons) > len(model.SecurityReasonCodes()) {
		return false
	}
	allowed := model.SecurityReasonCodes()
	for _, reason := range reasons {
		if !slices.Contains(allowed, reason) {
			return false
		}
	}
	return true
}

func upsertSecurityFingerprints(ctx context.Context, tx *sql.Tx, fingerprints []SecurityFingerprint) error {
	for _, fingerprint := range fingerprints {
		if !securityFingerprintValid(fingerprint) {
			return ErrSecurityEventInvalid
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO security_login_fingerprints
			(actor_user_id,epoch,kind,fingerprint,first_seen_at,last_seen_at,success_count,retain_until)
			VALUES(?,?,?,?,?,?,1,?)
			ON CONFLICT(actor_user_id,epoch,kind,fingerprint) DO UPDATE SET
				last_seen_at=excluded.last_seen_at, success_count=MIN(success_count+1, 1000000)`,
			fingerprint.ActorUserID, fingerprint.Epoch, fingerprint.Kind, fingerprint.Fingerprint,
			fingerprint.SeenAt, fingerprint.SeenAt, fingerprint.RetainUntil)
		if err := securityRowsAffected(result, err, "upsert security fingerprint"); err != nil {
			return err
		}
	}
	return nil
}

func securityFingerprintValid(fingerprint SecurityFingerprint) bool {
	if fingerprint.ActorUserID == "" || len(fingerprint.Fingerprint) != 32 || fingerprint.SeenAt <= 0 {
		return false
	}
	if fingerprint.RetainUntil < fingerprint.SeenAt || fingerprint.Epoch == "" || len(fingerprint.Epoch) > 64 {
		return false
	}
	return fingerprint.Kind == SecurityFingerprintKindSource || fingerprint.Kind == SecurityFingerprintKindDevice
}

// SecurityFingerprintKindSource 与 SecurityFingerprintKindDevice 是唯一允许的指纹种类。
const (
	SecurityFingerprintKindSource = "source"
	SecurityFingerprintKindDevice = "device"
)

func securityRowsAffected(result sql.Result, err error, operation string) error {
	if err != nil {
		return errors.Join(ErrSecurityEventInvalid, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Join(ErrSecurityEventInvalid, err)
	}
	if affected != 1 {
		return ErrSecurityEventInvalid
	}
	return nil
}

var (
	securityEventKindValues  = []string{model.SecurityEventLoginSucceeded, model.SecurityEventLoginSuspicious}
	securitySeverityValues   = []string{model.SecuritySeverityInfo, model.SecuritySeverityMedium, model.SecuritySeverityHigh}
	securityAuthMethodValues = []string{model.SecurityAuthMethodPassword, model.SecurityAuthMethodOIDC,
		model.SecurityAuthMethodRecovery, model.SecurityAuthMethodBootstrap, model.SecurityAuthMethodUnknown}
	securityRoutingValues = []string{model.SecurityRoutingRouted, model.SecurityRoutingNotRouted,
		model.SecurityRoutingSkipped, model.SecurityRoutingUnavailable}
	securitySourceClassValues = []string{model.SecuritySourceClassLoopback, model.SecuritySourceClassPrivate,
		model.SecuritySourceClassLinkLocal, model.SecuritySourceClassPublic, model.SecuritySourceClassUnknown}
	securitySourceFamilyValues = []string{model.SecuritySourceFamilyIPv4, model.SecuritySourceFamilyIPv6,
		model.SecuritySourceFamilyUnknown}
)
