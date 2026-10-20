package store

import "context"

// MigrateGeoIP 建立 v13 独立应用状态，不修改节点遥测。
func MigrateGeoIP(ctx context.Context, db schemaExecutor) error {
	for _, statement := range geoIPSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

var geoIPSchema = []string{
	`ALTER TABLE application_secret_state RENAME TO application_secret_state_v12`,
	`CREATE TABLE application_secret_state (
 id INTEGER PRIMARY KEY CHECK(id=1),phase TEXT NOT NULL CHECK(phase IN('pending','backfill','sanitize','ready','rotating')),
 generation INTEGER NOT NULL CHECK(generation>=0),root_generation INTEGER NOT NULL CHECK(root_generation>=0),
 root_fingerprint TEXT NOT NULL,active_key_id TEXT NOT NULL,cursor_source INTEGER NOT NULL CHECK(cursor_source BETWEEN 0 AND 14),
 cursor_id TEXT NOT NULL,rotation_id TEXT NOT NULL DEFAULT '' CHECK(rotation_id='' OR length(rotation_id)=36),updated_at INTEGER NOT NULL)`,
	`INSERT INTO application_secret_state SELECT * FROM application_secret_state_v12`,
	`DROP TABLE application_secret_state_v12`,
	`CREATE TABLE geoip_settings (
 id TEXT PRIMARY KEY CHECK(id='global'), provider TEXT NOT NULL DEFAULT 'disabled', revision INTEGER NOT NULL DEFAULT 1,
 remote_consent INTEGER NOT NULL DEFAULT 0 CHECK(remote_consent IN(0,1)), credential_encrypted BLOB,
 mmdb_source TEXT NOT NULL DEFAULT '', mmdb_edition TEXT NOT NULL DEFAULT '', mmdb_build_epoch INTEGER NOT NULL DEFAULT 0,
 mmdb_sha256 TEXT NOT NULL DEFAULT '', mmdb_size INTEGER NOT NULL DEFAULT 0,
 mmdb_state TEXT NOT NULL DEFAULT 'missing', mmdb_error TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL)`,
	`INSERT INTO geoip_settings(id,updated_at) VALUES('global',CURRENT_TIMESTAMP)`,
	`CREATE TABLE node_locations (
 id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 mode TEXT NOT NULL DEFAULT 'observed' CHECK(mode IN('observed','managed_target','manual','disabled')),
 source_encrypted BLOB, source_fingerprint TEXT NOT NULL DEFAULT '', scope TEXT NOT NULL DEFAULT 'invalid',
 country_code TEXT NOT NULL DEFAULT '', visibility TEXT NOT NULL DEFAULT 'inherit' CHECK(visibility IN('inherit','public','private')),
 provider TEXT NOT NULL DEFAULT '', revision TEXT NOT NULL DEFAULT '', resolved_at DATETIME, last_attempt_at DATETIME,
 next_refresh_at DATETIME, error_code TEXT NOT NULL DEFAULT '', failures INTEGER NOT NULL DEFAULT 0,
 lease_hash TEXT NOT NULL DEFAULT '', lease_expires_at DATETIME, worker_id TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL)`,
	`INSERT INTO node_locations(id,updated_at) SELECT id,CURRENT_TIMESTAMP FROM nodes`,
	`CREATE INDEX idx_node_locations_due ON node_locations(next_refresh_at,lease_expires_at)`,
	`CREATE TABLE geoip_cache (
 provider_revision TEXT NOT NULL, source_fingerprint TEXT NOT NULL, scope TEXT NOT NULL,
 country_code TEXT NOT NULL DEFAULT '', negative INTEGER NOT NULL CHECK(negative IN(0,1)),
 resolved_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, accessed_at DATETIME NOT NULL,
 PRIMARY KEY(provider_revision,source_fingerprint))`,
	`CREATE INDEX idx_geoip_cache_lru ON geoip_cache(accessed_at)`,
	`CREATE TABLE lifecycle_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), timezone TEXT NOT NULL DEFAULT 'UTC', lead_days TEXT NOT NULL DEFAULT '[0,1,3,7,14,30]',
 post_days TEXT NOT NULL DEFAULT '[1,7]', public_country INTEGER NOT NULL DEFAULT 0,
 public_expiry INTEGER NOT NULL DEFAULT 0, public_price INTEGER NOT NULL DEFAULT 0,
 last_success_at DATETIME, updated_at DATETIME NOT NULL)`,
	`INSERT INTO lifecycle_settings(id,updated_at) VALUES(1,CURRENT_TIMESTAMP)`,
	`CREATE TABLE node_lifecycle (
 id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE, provider TEXT NOT NULL DEFAULT '', plan TEXT NOT NULL DEFAULT '',
 price_micros INTEGER CHECK(price_micros>=0), currency TEXT NOT NULL DEFAULT '', interval_unit TEXT NOT NULL DEFAULT 'month',
 interval_count INTEGER NOT NULL DEFAULT 1 CHECK(interval_count BETWEEN 1 AND 120), expires_on TEXT NOT NULL DEFAULT '', period_key TEXT NOT NULL DEFAULT '',
 renewal_mode TEXT NOT NULL DEFAULT 'manual' CHECK(renewal_mode IN('manual','calendar_auto_advance')),
 anchor_month INTEGER NOT NULL DEFAULT 0, anchor_day INTEGER NOT NULL DEFAULT 0, anchor_end_of_month INTEGER NOT NULL DEFAULT 0,
 private_notes TEXT NOT NULL DEFAULT '', expiry_visibility TEXT NOT NULL DEFAULT 'inherit', price_visibility TEXT NOT NULL DEFAULT 'inherit',
 created_by TEXT NOT NULL, updated_by TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
	`CREATE INDEX idx_node_lifecycle_expiry ON node_lifecycle(expires_on)`,
	`CREATE TABLE node_lifecycle_events (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL, node_name TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
 period_key TEXT NOT NULL, stage INTEGER NOT NULL, old_expires_on TEXT NOT NULL DEFAULT '', new_expires_on TEXT NOT NULL DEFAULT '',
 skipped_intervals INTEGER NOT NULL DEFAULT 0, actor TEXT NOT NULL, routing_status TEXT NOT NULL,
 message_id TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL,
 UNIQUE(node_id,period_key,kind,stage))`,
	`CREATE INDEX idx_node_lifecycle_events_node ON node_lifecycle_events(node_id,created_at)`,
}
