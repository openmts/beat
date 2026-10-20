package store

import "context"

// MigrateNotifications 只执行 v10 DDL；旧明文由共享 secret coordinator 在监听前转换。
func MigrateNotifications(ctx context.Context, database schemaExecutor) error {
	if err := executeSchemaStatements(ctx, database, notificationChannelSchema); err != nil {
		return err
	}
	if err := executeSchemaStatements(ctx, database, notificationQueueSchema); err != nil {
		return err
	}
	return executeSchemaStatements(ctx, database, notificationLifecycleSchema)
}

var notificationChannelSchema = []string{
	`ALTER TABLE alert_channels ADD COLUMN current_revision INTEGER NOT NULL DEFAULT 0 CHECK(current_revision>=0)`,
	`ALTER TABLE alert_channels ADD COLUMN event_kinds_json TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE alert_channels ADD COLUMN deleted_at DATETIME`,
	`CREATE TABLE alert_channel_revisions (
		id TEXT NOT NULL UNIQUE,
		channel_id TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0),
		channel_type TEXT NOT NULL CHECK(channel_type IN ('webhook','telegram','email','bark','serverchan_turbo','serverchan_3','custom_http')),
		public_config_json TEXT NOT NULL CHECK(length(CAST(public_config_json AS BLOB))<=32768),
		secret_ciphertext BLOB NOT NULL CHECK(length(secret_ciphertext) BETWEEN 58 AND 16442),
		secret_present_json TEXT NOT NULL,
		template_subject TEXT NOT NULL DEFAULT '', template_text TEXT NOT NULL DEFAULT '',
		template_version INTEGER NOT NULL DEFAULT 1 CHECK(template_version=1),
		created_by TEXT NOT NULL, created_at INTEGER NOT NULL, change_reason TEXT NOT NULL,
		PRIMARY KEY(channel_id,revision), FOREIGN KEY(channel_id) REFERENCES alert_channels(id) ON DELETE RESTRICT
	)`,
	`CREATE TABLE notification_channel_health (
		channel_id TEXT PRIMARY KEY, current_revision INTEGER NOT NULL,
		consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0),
		circuit TEXT NOT NULL DEFAULT 'closed' CHECK(circuit IN ('closed','open')),
		open_until INTEGER, last_success INTEGER, last_failure INTEGER,
		error_code TEXT NOT NULL DEFAULT '', error_stage TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL,
		FOREIGN KEY(channel_id) REFERENCES alert_channels(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE notification_settings (
		id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL CHECK(revision>0),
		success_retention_days INTEGER NOT NULL DEFAULT 30 CHECK(success_retention_days BETWEEN 7 AND 365),
		dead_retention_days INTEGER NOT NULL DEFAULT 90 CHECK(dead_retention_days BETWEEN 30 AND 730),
		maximum_workers INTEGER NOT NULL DEFAULT 8 CHECK(maximum_workers BETWEEN 1 AND 32),
		egress_policy_json TEXT NOT NULL DEFAULT '{"revision":1,"exceptions":[]}', updated_at INTEGER NOT NULL
	)`,
	`INSERT INTO notification_settings(id,revision,updated_at) VALUES(1,1,0)`,
	`CREATE TABLE notification_request_budgets (
		kind TEXT NOT NULL, scope TEXT NOT NULL, window_start INTEGER NOT NULL,
		request_count INTEGER NOT NULL CHECK(request_count>=0),
		PRIMARY KEY(kind,scope,window_start)
	)`,
}

var notificationQueueSchema = []string{
	`CREATE TABLE notification_messages (
		id TEXT PRIMARY KEY, source_key TEXT NOT NULL UNIQUE CHECK(length(source_key) BETWEEN 1 AND 512),
		source_kind TEXT NOT NULL, source_id TEXT NOT NULL, event_kind TEXT NOT NULL,
		payload_encrypted BLOB NOT NULL CHECK(length(payload_encrypted) BETWEEN 58 AND 65594),
		payload_version INTEGER NOT NULL CHECK(payload_version=1),
		routing_state TEXT NOT NULL CHECK(routing_state IN ('routed','not_routed')),
		selected_channels INTEGER NOT NULL CHECK(selected_channels BETWEEN 0 AND 256),
		created_at INTEGER NOT NULL, retain_until INTEGER NOT NULL
	)`,
	`CREATE INDEX notification_message_retention ON notification_messages(retain_until)`,
	`CREATE TABLE notification_deliveries (
		id TEXT PRIMARY KEY, message_id TEXT NOT NULL, channel_id TEXT NOT NULL, channel_revision INTEGER NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('pending','leased','succeeded','dead','cancelled')),
		attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count BETWEEN 0 AND 6),
		next_attempt_at INTEGER NOT NULL, lease_token_hash BLOB, lease_expires_at INTEGER, worker_id TEXT NOT NULL DEFAULT '',
		error_code TEXT NOT NULL DEFAULT '', error_stage TEXT NOT NULL DEFAULT '', status_class TEXT NOT NULL DEFAULT '',
		retry_of TEXT UNIQUE, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, completed_at INTEGER,
		CHECK((state='leased' AND length(lease_token_hash)=32 AND lease_expires_at IS NOT NULL) OR
			(state<>'leased' AND lease_token_hash IS NULL AND lease_expires_at IS NULL)),
		FOREIGN KEY(message_id) REFERENCES notification_messages(id) ON DELETE CASCADE,
		FOREIGN KEY(channel_id,channel_revision) REFERENCES alert_channel_revisions(channel_id,revision) ON DELETE RESTRICT,
		FOREIGN KEY(retry_of) REFERENCES notification_deliveries(id) ON DELETE RESTRICT
	)`,
	`CREATE UNIQUE INDEX notification_original_delivery ON notification_deliveries(message_id,channel_id) WHERE retry_of IS NULL`,
	`CREATE UNIQUE INDEX notification_channel_singleflight ON notification_deliveries(channel_id) WHERE state='leased'`,
	`CREATE INDEX notification_delivery_due ON notification_deliveries(state,next_attempt_at,created_at)`,
	`CREATE INDEX notification_delivery_message ON notification_deliveries(message_id,channel_id)`,
	`CREATE INDEX notification_delivery_channel_order ON notification_deliveries(channel_id,created_at,id)`,
	`CREATE TABLE notification_delivery_attempts (
		delivery_id TEXT NOT NULL, attempt_number INTEGER NOT NULL CHECK(attempt_number BETWEEN 1 AND 6),
		started_at INTEGER NOT NULL, completed_at INTEGER, duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(duration_ms>=0),
		outcome TEXT NOT NULL CHECK(outcome IN ('in_progress','succeeded','retryable','permanent','cancelled','lease_expired')),
		error_code TEXT NOT NULL DEFAULT '', error_stage TEXT NOT NULL DEFAULT '', status_class TEXT NOT NULL DEFAULT '',
		PRIMARY KEY(delivery_id,attempt_number), FOREIGN KEY(delivery_id) REFERENCES notification_deliveries(id) ON DELETE CASCADE
	)`,
}

var notificationLifecycleSchema = []string{
	`CREATE TABLE notification_secret_state (id INTEGER PRIMARY KEY CHECK(id=1),
		phase TEXT NOT NULL CHECK(phase IN ('pending','sanitize','ready')), updated_at INTEGER NOT NULL)`,
	`INSERT INTO notification_secret_state VALUES(1,'pending',0)`,
	`ALTER TABLE application_secret_state RENAME TO application_secret_state_v9`,
	`CREATE TABLE application_secret_state (
		id INTEGER PRIMARY KEY CHECK(id=1), phase TEXT NOT NULL CHECK(phase IN ('pending','backfill','sanitize','ready','rotating')),
		generation INTEGER NOT NULL CHECK(generation>=0), root_generation INTEGER NOT NULL CHECK(root_generation>=0),
		root_fingerprint TEXT NOT NULL, active_key_id TEXT NOT NULL,
		cursor_source INTEGER NOT NULL CHECK(cursor_source BETWEEN 0 AND 6), cursor_id TEXT NOT NULL,
		rotation_id TEXT NOT NULL DEFAULT '' CHECK(rotation_id='' OR length(rotation_id)=36), updated_at INTEGER NOT NULL
	)`,
	`INSERT INTO application_secret_state SELECT * FROM application_secret_state_v9`,
	`DROP TABLE application_secret_state_v9`,
}
