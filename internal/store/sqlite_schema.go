package store

var platformSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS groups (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, sort_order INTEGER DEFAULT 0,
		is_default INTEGER DEFAULT 0, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, alias TEXT, group_id TEXT,
		host TEXT NOT NULL, port INTEGER NOT NULL, status TEXT DEFAULT 'offline', ssh_public_key TEXT,
		cpu_model TEXT NOT NULL DEFAULT '', os TEXT NOT NULL DEFAULT '', platform TEXT NOT NULL DEFAULT '',
		os_version TEXT NOT NULL DEFAULT '', kernel TEXT NOT NULL DEFAULT '', arch TEXT NOT NULL DEFAULT '',
		virtualization TEXT NOT NULL DEFAULT '', agent_version TEXT NOT NULL DEFAULT '',
		sort_order INTEGER NOT NULL DEFAULT 0 CHECK(sort_order >= 0), tags TEXT NOT NULL DEFAULT '[]',
		is_public INTEGER NOT NULL DEFAULT 1 CHECK(is_public IN (0, 1)), public_remark TEXT NOT NULL DEFAULT '',
		private_remark TEXT NOT NULL DEFAULT '', agent_token_hash BLOB, agent_token_prefix TEXT NOT NULL DEFAULT '',
		agent_token_created_at DATETIME, agent_token_last_used_at DATETIME, agent_token_revoked_at DATETIME,
		traffic_limit INTEGER NOT NULL DEFAULT 0, traffic_limit_type TEXT NOT NULL DEFAULT 'sum',
		traffic_reset_day INTEGER NOT NULL DEFAULT 1, last_seen DATETIME, created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL, FOREIGN KEY(group_id) REFERENCES groups(id)
	)`,
	`CREATE TABLE IF NOT EXISTS ssh_keys (
		id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL, key_type TEXT NOT NULL,
		public_key TEXT NOT NULL, private_key TEXT NOT NULL, fingerprint TEXT, created_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS alert_rules (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT, metric TEXT NOT NULL,
		operator TEXT NOT NULL, threshold REAL NOT NULL, duration INTEGER DEFAULT 0,
		severity TEXT DEFAULT 'warning', enabled INTEGER DEFAULT 1,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS alert_channels (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, channel_type TEXT NOT NULL, config TEXT,
		enabled INTEGER DEFAULT 1, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS alert_events (
		id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, node_id TEXT NOT NULL, message TEXT,
		value REAL, status TEXT NOT NULL, triggered_at DATETIME NOT NULL, resolved_at DATETIME
	)`,
	`CREATE TABLE IF NOT EXISTS traffic_report_schedules (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, cadence TEXT NOT NULL CHECK(cadence IN ('daily', 'weekly', 'monthly')),
		timezone TEXT NOT NULL, send_hour INTEGER NOT NULL CHECK(send_hour BETWEEN 0 AND 23),
		send_minute INTEGER NOT NULL CHECK(send_minute BETWEEN 0 AND 59),
		weekday INTEGER NOT NULL DEFAULT 1 CHECK(weekday BETWEEN 1 AND 7),
		month_day INTEGER NOT NULL DEFAULT 1 CHECK(month_day BETWEEN 1 AND 31),
		all_nodes INTEGER NOT NULL CHECK(all_nodes IN (0, 1)), all_channels INTEGER NOT NULL CHECK(all_channels IN (0, 1)),
		enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)), last_period_key TEXT NOT NULL DEFAULT '',
		last_run_at DATETIME, next_run_at DATETIME NOT NULL, last_delivery_state TEXT NOT NULL DEFAULT '',
		last_delivery_message TEXT NOT NULL DEFAULT '', last_delivery_delivered INTEGER NOT NULL DEFAULT 0,
		last_delivery_total INTEGER NOT NULL DEFAULT 0, last_delivery_at DATETIME,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS traffic_report_schedule_nodes (
		schedule_id TEXT NOT NULL, node_id TEXT NOT NULL, PRIMARY KEY(schedule_id, node_id),
		FOREIGN KEY(schedule_id) REFERENCES traffic_report_schedules(id) ON DELETE CASCADE,
		FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS traffic_report_schedule_channels (
		schedule_id TEXT NOT NULL, channel_id TEXT NOT NULL, PRIMARY KEY(schedule_id, channel_id),
		FOREIGN KEY(schedule_id) REFERENCES traffic_report_schedules(id) ON DELETE CASCADE,
		FOREIGN KEY(channel_id) REFERENCES alert_channels(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_traffic_report_schedules_due ON traffic_report_schedules(enabled, next_run_at)`,
	`CREATE TABLE IF NOT EXISTS network_tasks (
		id TEXT PRIMARY KEY, name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
		type TEXT NOT NULL CHECK(type IN ('icmp', 'tcp', 'http')),
		target TEXT NOT NULL CHECK(length(CAST(target AS BLOB)) BETWEEN 1 AND 2048),
		ip_family TEXT NOT NULL CHECK(ip_family IN ('auto', 'ipv4', 'ipv6')),
		interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 10 AND 86400),
		timeout_milliseconds INTEGER NOT NULL CHECK(timeout_milliseconds BETWEEN 100 AND 30000),
		all_nodes INTEGER NOT NULL CHECK(all_nodes IN (0, 1)), enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
		is_public INTEGER NOT NULL CHECK(is_public IN (0, 1)), sort_order INTEGER NOT NULL CHECK(sort_order >= 0),
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		CHECK(timeout_milliseconds <= interval_seconds * 1000)
	)`,
	`CREATE TABLE IF NOT EXISTS network_task_nodes (
		task_id TEXT NOT NULL, node_id TEXT NOT NULL, PRIMARY KEY(task_id, node_id),
		FOREIGN KEY(task_id) REFERENCES network_tasks(id) ON DELETE CASCADE,
		FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_network_tasks_visibility ON network_tasks(enabled, is_public, sort_order)`,
	`CREATE INDEX IF NOT EXISTS idx_network_task_nodes_node ON network_task_nodes(node_id, task_id)`,
	`CREATE TABLE IF NOT EXISTS site_settings (
		id INTEGER PRIMARY KEY CHECK(id = 1), site_title TEXT NOT NULL CHECK(length(site_title) BETWEEN 1 AND 80),
		site_description TEXT NOT NULL CHECK(length(site_description) <= 240),
		logo_url TEXT NOT NULL CHECK(length(CAST(logo_url AS BLOB)) <= 2048),
		favicon_url TEXT NOT NULL CHECK(length(CAST(favicon_url AS BLOB)) <= 2048),
		default_theme TEXT NOT NULL CHECK(default_theme IN ('system', 'light', 'dark')),
		show_ip_addresses INTEGER NOT NULL CHECK(show_ip_addresses IN (0, 1)),
		show_network_quality INTEGER NOT NULL CHECK(show_network_quality IN (0, 1)), updated_at DATETIME NOT NULL
	)`,
	`INSERT OR IGNORE INTO site_settings (
		id, site_title, site_description, logo_url, favicon_url, default_theme,
		show_ip_addresses, show_network_quality, updated_at
	) VALUES (1, 'Beat Monitor', 'Server monitoring and operations dashboard.',
		'', '/favicon.svg', 'system', 1, 1, CURRENT_TIMESTAMP)`,
	`CREATE TABLE IF NOT EXISTS maintenance_settings (
		id INTEGER PRIMARY KEY CHECK(id = 1), retention_days INTEGER NOT NULL CHECK(retention_days BETWEEN 1 AND 3650),
		auto_cleanup_enabled INTEGER NOT NULL CHECK(auto_cleanup_enabled IN (0, 1)),
		cleanup_hour_utc INTEGER NOT NULL CHECK(cleanup_hour_utc BETWEEN 0 AND 23),
		running INTEGER NOT NULL DEFAULT 0 CHECK(running IN (0, 1)), last_started_at DATETIME,
		last_completed_at DATETIME, last_status TEXT NOT NULL DEFAULT 'never'
			CHECK(last_status IN ('never', 'running', 'success', 'failed')), last_error TEXT NOT NULL DEFAULT '',
		last_cutoff_at DATETIME, last_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(last_duration_ms >= 0),
		last_trigger TEXT NOT NULL DEFAULT '' CHECK(last_trigger IN ('', 'manual', 'automatic')),
		sqlite_integrity TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL
	)`,
	`INSERT OR IGNORE INTO maintenance_settings (
		id, retention_days, auto_cleanup_enabled, cleanup_hour_utc, updated_at
	) VALUES (1, 30, 1, 3, CURRENT_TIMESTAMP)`,
}
