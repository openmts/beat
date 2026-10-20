package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrSecuritySettingsRevision 表示策略在读取后被其他管理员修改。
var ErrSecuritySettingsRevision = errors.New("store: security notification settings changed")

// ErrSecuritySettingsInvalid 表示策略字段超出允许的有界范围。
var ErrSecuritySettingsInvalid = errors.New("store: security notification settings are invalid")

const (
	securitySettingsColumns = `enabled_event_kinds, privacy_level, lookback_days, threshold_failures,
		threshold_window_seconds, invalid_totp_threshold, retention_days, fingerprint_key, fingerprint_epoch,
		revision, updated_at, updated_by`
)

var securitySettingsKinds = []string{model.SecurityEventLoginSucceeded, model.SecurityEventLoginSuspicious}

// SecurityCredentials 是 HMAC 指纹密钥与纪元；轮换纪元会重新开始来源比较。
type SecurityCredentials struct {
	FingerprintKey   []byte
	FingerprintEpoch string
}

func (store *AdminStore) SecurityNotificationSettings(ctx context.Context) (model.SecurityNotificationSettings, error) {
	settings, _, err := scanSecuritySettings(store.db.QueryRowContext(ctx,
		"SELECT "+securitySettingsColumns+" FROM security_notification_settings WHERE id=1"))
	if err != nil {
		return model.SecurityNotificationSettings{}, fmt.Errorf("read security notification settings: %w", err)
	}
	return settings, nil
}

func (store *AdminStore) SecurityCredentials(ctx context.Context) (SecurityCredentials, error) {
	_, credentials, err := scanSecuritySettings(store.db.QueryRowContext(ctx,
		"SELECT "+securitySettingsColumns+" FROM security_notification_settings WHERE id=1"))
	if err != nil {
		return SecurityCredentials{}, fmt.Errorf("read security fingerprint credentials: %w", err)
	}
	return credentials, nil
}

func scanSecuritySettings(row rowScanner) (model.SecurityNotificationSettings, SecurityCredentials, error) {
	var (
		settings    model.SecurityNotificationSettings
		credentials SecurityCredentials
		encoded     string
		updatedAt   int64
	)
	err := row.Scan(&encoded, &settings.PrivacyLevel, &settings.LookbackDays, &settings.ThresholdFailures,
		&settings.ThresholdWindowSecond, &settings.InvalidTOTPThreshold, &settings.RetentionDays,
		&credentials.FingerprintKey, &credentials.FingerprintEpoch, &settings.Revision, &updatedAt, &settings.UpdatedBy)
	if err != nil {
		return model.SecurityNotificationSettings{}, SecurityCredentials{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &settings.EnabledEventKinds); err != nil {
		return model.SecurityNotificationSettings{}, SecurityCredentials{}, err
	}
	settings.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return settings, credentials, nil
}

// SecuritySettingsChange 一次策略变更连同审计在同一事务内提交。
type SecuritySettingsChange struct {
	Write     model.SecurityNotificationSettingsWrite
	Audit     model.AdminAuditEvent
	UpdatedBy string
	Now       time.Time
}

func (store *AdminStore) UpdateSecurityNotificationSettings(
	ctx context.Context, change SecuritySettingsChange,
) (model.SecurityNotificationSettings, error) {
	encoded, err := validateSecuritySettingsWrite(change)
	if err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := updateSecuritySettingsTx(ctx, tx, change, encoded); err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	if err := insertAuditEvent(ctx, tx, &change.Audit); err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	settings, _, err := scanSecuritySettings(tx.QueryRowContext(ctx,
		"SELECT "+securitySettingsColumns+" FROM security_notification_settings WHERE id=1"))
	if err != nil {
		return model.SecurityNotificationSettings{}, fmt.Errorf("reread security notification settings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.SecurityNotificationSettings{}, err
	}
	return settings, nil
}

func updateSecuritySettingsTx(
	ctx context.Context, tx *sql.Tx, change SecuritySettingsChange, encoded string,
) error {
	var previous string
	var revision int64
	if err := tx.QueryRowContext(ctx,
		"SELECT json_object('enabled_event_kinds', enabled_event_kinds, 'privacy_level', privacy_level,"+
			" 'lookback_days', lookback_days, 'threshold_failures', threshold_failures,"+
			" 'threshold_window_seconds', threshold_window_seconds, 'invalid_totp_threshold', invalid_totp_threshold,"+
			" 'retention_days', retention_days), revision FROM security_notification_settings WHERE id=1",
	).Scan(&previous, &revision); err != nil {
		return fmt.Errorf("read current security notification settings: %w", err)
	}
	if change.Write.ExpectedRevision != revision {
		return ErrSecuritySettingsRevision
	}
	result, err := tx.ExecContext(ctx, `UPDATE security_notification_settings SET
		enabled_event_kinds=?, privacy_level=?, lookback_days=?, threshold_failures=?, threshold_window_seconds=?,
		invalid_totp_threshold=?, retention_days=?, revision=revision+1, updated_at=?, updated_by=?, previous_json=?
		WHERE id=1 AND revision=?`, encoded, change.Write.PrivacyLevel, change.Write.LookbackDays,
		change.Write.ThresholdFailures, change.Write.ThresholdWindowSecond, change.Write.InvalidTOTPThreshold,
		change.Write.RetentionDays, change.Now.UnixMilli(), change.UpdatedBy, previous, revision)
	if err := sessionRowsAffected(result, err, "update security notification settings"); err != nil {
		return err
	}
	return nil
}

func validateSecuritySettingsWrite(change SecuritySettingsChange) (string, error) {
	write := change.Write
	if write.ExpectedRevision <= 0 || change.UpdatedBy == "" || len(change.UpdatedBy) > 128 ||
		write.PrivacyLevel != "coarse" && write.PrivacyLevel != "full_source" ||
		write.LookbackDays < 1 || write.LookbackDays > 365 ||
		write.ThresholdFailures < 1 || write.ThresholdFailures > 100 ||
		write.ThresholdWindowSecond < 60 || write.ThresholdWindowSecond > 86400 ||
		write.InvalidTOTPThreshold < 1 || write.InvalidTOTPThreshold > 100 ||
		write.RetentionDays < 30 || write.RetentionDays > 730 ||
		change.Audit.ID == "" || change.Audit.CreatedAt.IsZero() {
		return "", ErrSecuritySettingsInvalid
	}
	selected, err := json.Marshal(write.EnabledEventKinds)
	if err != nil || !validSecurityKinds(write.EnabledEventKinds) {
		return "", ErrSecuritySettingsInvalid
	}
	return string(selected), nil
}

func validSecurityKinds(kinds []string) bool {
	if len(kinds) == 0 || len(kinds) > len(securitySettingsKinds) {
		return false
	}
	seen := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		if !slices.Contains(securitySettingsKinds, kind) || seen[kind] {
			return false
		}
		seen[kind] = true
	}
	return true
}
