package store

import "context"

// MigrateLoginSecurity 登记 v17 登录安全事件、登录指纹、持久化阈值与通知策略。
// 四张表都只存应用侧元数据：不含口令哈希、原始凭证、原始 User-Agent 或会话令牌。
func MigrateLoginSecurity(ctx context.Context, db schemaExecutor) error {
	return executeSchemaStatements(ctx, db, securityLoginSchema)
}

var securityLoginSchema = []string{
	`CREATE TABLE security_events (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL CHECK(kind IN ('security.login_succeeded','security.login_suspicious')),
		severity TEXT NOT NULL CHECK(severity IN ('info','medium','high')),
		actor_user_id TEXT REFERENCES admin_users(id) ON DELETE SET NULL,
		actor_display_name TEXT NOT NULL DEFAULT '' CHECK(length(actor_display_name) <= 200),
		auth_method TEXT NOT NULL DEFAULT 'unknown'
			CHECK(auth_method IN ('password','oidc','recovery','bootstrap','unknown')),
		source_class TEXT NOT NULL DEFAULT 'unknown'
			CHECK(source_class IN ('loopback','private','link_local','public','unknown')),
		source_family TEXT NOT NULL DEFAULT 'unknown' CHECK(source_family IN ('ipv4','ipv6','unknown')),
		source_prefix TEXT NOT NULL DEFAULT '' CHECK(length(source_prefix) <= 128),
		browser_family TEXT NOT NULL DEFAULT 'unknown' CHECK(length(browser_family) <= 64),
		os_family TEXT NOT NULL DEFAULT 'unknown' CHECK(length(os_family) <= 64),
		device_class TEXT NOT NULL DEFAULT 'unknown' CHECK(length(device_class) <= 32),
		country_code TEXT NOT NULL DEFAULT '' CHECK(length(country_code) <= 8),
		region TEXT NOT NULL DEFAULT '' CHECK(length(region) <= 128),
		reasons_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(reasons_json)),
		policy_revision INTEGER NOT NULL CHECK(policy_revision >= 0),
		source_key TEXT NOT NULL UNIQUE CHECK(length(source_key) BETWEEN 1 AND 512),
		source_kind TEXT NOT NULL DEFAULT '' CHECK(length(source_kind) <= 64),
		source_id TEXT NOT NULL DEFAULT '' CHECK(length(source_id) <= 128),
		message_id TEXT REFERENCES notification_messages(id) ON DELETE SET NULL,
		routing_state TEXT NOT NULL DEFAULT 'not_routed'
			CHECK(routing_state IN ('routed','not_routed','skipped','unavailable')),
		audit_event_id TEXT NOT NULL DEFAULT '' CHECK(length(audit_event_id) <= 128),
		occurred_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		retain_until INTEGER NOT NULL,
		acknowledged_at INTEGER,
		acknowledged_by TEXT,
		acknowledged_note TEXT NOT NULL DEFAULT '' CHECK(length(acknowledged_note) <= 500)
	)`,
	`CREATE INDEX security_event_history ON security_events(occurred_at DESC, id DESC)`,
	`CREATE INDEX security_event_actor ON security_events(actor_user_id, occurred_at DESC)`,
	`CREATE INDEX security_event_kind ON security_events(kind, severity, occurred_at DESC)`,
	`CREATE INDEX security_event_retention ON security_events(retain_until)`,
	`CREATE TABLE security_login_fingerprints (
		actor_user_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
		epoch TEXT NOT NULL CHECK(length(epoch) BETWEEN 1 AND 64),
		kind TEXT NOT NULL CHECK(kind IN ('source','device')),
		fingerprint BLOB NOT NULL CHECK(length(fingerprint) = 32),
		first_seen_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL,
		success_count INTEGER NOT NULL DEFAULT 1 CHECK(success_count BETWEEN 1 AND 1000000),
		retain_until INTEGER NOT NULL,
		PRIMARY KEY(actor_user_id, epoch, kind, fingerprint)
	)`,
	`CREATE INDEX security_fingerprint_retention ON security_login_fingerprints(retain_until)`,
	`CREATE TABLE security_login_thresholds (
		key_kind TEXT NOT NULL CHECK(key_kind IN ('source','username')),
		key_hash BLOB NOT NULL CHECK(length(key_hash) = 32),
		window_started_at INTEGER NOT NULL,
		attempt_count INTEGER NOT NULL CHECK(attempt_count BETWEEN 1 AND 100000),
		lock_expires_at INTEGER,
		last_seen_at INTEGER NOT NULL,
		last_emitted_at INTEGER,
		PRIMARY KEY(key_kind, key_hash, window_started_at)
	)`,
	`CREATE INDEX security_threshold_expiry ON security_login_thresholds(last_seen_at, lock_expires_at)`,
	`CREATE INDEX security_threshold_dedupe ON security_login_thresholds(key_kind, key_hash, window_started_at DESC)`,
	`CREATE TABLE security_notification_settings (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		enabled_event_kinds TEXT NOT NULL CHECK(json_valid(enabled_event_kinds)),
		privacy_level TEXT NOT NULL CHECK(privacy_level IN ('coarse','full_source')),
		lookback_days INTEGER NOT NULL CHECK(lookback_days BETWEEN 1 AND 365),
		threshold_failures INTEGER NOT NULL CHECK(threshold_failures BETWEEN 1 AND 100),
		threshold_window_seconds INTEGER NOT NULL CHECK(threshold_window_seconds BETWEEN 60 AND 86400),
		invalid_totp_threshold INTEGER NOT NULL CHECK(invalid_totp_threshold BETWEEN 1 AND 100),
		retention_days INTEGER NOT NULL CHECK(retention_days BETWEEN 30 AND 730),
		fingerprint_key BLOB NOT NULL CHECK(length(fingerprint_key) = 32),
		fingerprint_epoch TEXT NOT NULL CHECK(length(fingerprint_epoch) BETWEEN 1 AND 64),
		revision INTEGER NOT NULL CHECK(revision > 0),
		updated_at INTEGER NOT NULL,
		updated_by TEXT NOT NULL DEFAULT '' CHECK(length(updated_by) <= 128),
		previous_json TEXT CHECK(previous_json IS NULL OR json_valid(previous_json))
	)`,
	`INSERT INTO security_notification_settings (
		id, enabled_event_kinds, privacy_level, lookback_days, threshold_failures,
		threshold_window_seconds, invalid_totp_threshold, retention_days,
		fingerprint_key, fingerprint_epoch, revision, updated_at, updated_by
	) VALUES (1, '["security.login_succeeded","security.login_suspicious"]', 'coarse', 30, 5, 900, 5, 180,
		randomblob(32), 'epoch-1', 1, 0, '')`,
}
