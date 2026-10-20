package store

var reportPreparationSchema = []string{
	`ALTER TABLE traffic_report_schedules ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0)`,
	`CREATE TABLE traffic_report_preparations(
		schedule_id TEXT NOT NULL,period_key TEXT NOT NULL CHECK(length(period_key) BETWEEN 1 AND 128),
		schedule_revision INTEGER NOT NULL CHECK(schedule_revision>0),state_version INTEGER NOT NULL CHECK(state_version>0),
		outcome TEXT NOT NULL CHECK(outcome IN ('preparing','failed','queued','superseded')),
		attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count>=0),
		reason_code TEXT NOT NULL DEFAULT '' CHECK(length(reason_code)<=64),
		last_attempt_at INTEGER NOT NULL,next_retry_at INTEGER NOT NULL,
		lease_token_hash BLOB,lease_expires_at INTEGER,worker_id TEXT NOT NULL DEFAULT '' CHECK(length(worker_id)<=36),
		message_source_key TEXT NOT NULL DEFAULT '' CHECK(length(message_source_key)<=512),
		completed_at INTEGER,updated_at INTEGER NOT NULL,
		CHECK((outcome='preparing' AND length(lease_token_hash)=32 AND lease_expires_at IS NOT NULL AND worker_id<>'') OR
			(outcome<>'preparing' AND lease_token_hash IS NULL AND lease_expires_at IS NULL AND worker_id='')),
		PRIMARY KEY(schedule_id,period_key)
	)`,
	`CREATE INDEX traffic_preparation_retry ON traffic_report_preparations(outcome,next_retry_at,schedule_id)`,
}
