package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const currentSchemaVersion = 30

type sqliteMigration struct {
	version int
	name    string
	apply   func(context.Context, schemaExecutor) error
}

var sqliteMigrations = []sqliteMigration{
	{version: 1, name: "application_schema_baseline", apply: applyApplicationSchema},
	{version: 2, name: "agent_task_channel", apply: applyAgentTaskChannel},
	{version: 3, name: "custom_metric_tasks", apply: applyCustomMetricTasks},
	{version: 4, name: "alert_event_acknowledgement", apply: applyAlertEventAcknowledgement},
	{version: 5, name: "oidc_identity", apply: applyOIDCIdentity},
	{version: 6, name: "theme_packages", apply: applyThemePackages},
	{version: 7, name: "agent_enrollment", apply: applyAgentEnrollment},
	{version: 8, name: "metric_catalog_dashboards", apply: applyMetricCatalog},
	{version: 9, name: "application_secrets", apply: MigrateApplicationSecrets},
	{version: 10, name: "notification_delivery", apply: MigrateNotifications},
	{version: 11, name: "remote_operations", apply: MigrateOperations},
	{version: 12, name: "alert_evaluation", apply: MigrateAlertEvaluation},
	{version: 13, name: "geoip_node_lifecycle", apply: MigrateGeoIP},
	{version: 14, name: "accelerator_telemetry", apply: MigrateAccelerators},
	{version: 15, name: "agent_rollout", apply: MigrateAgentRollout},
	{version: 16, name: "service_principals", apply: MigrateServicePrincipals},
	{version: 17, name: "login_security", apply: MigrateLoginSecurity},
	{version: 18, name: "resumable_restore_upload", apply: MigrateBackupUpload},
	{version: 19, name: "advanced_diagnostics", apply: MigrateAdvancedDiagnostics},
	{version: 20, name: "edge_access_policy", apply: MigrateEdgeAccessPolicy},
	{version: 21, name: "traffic_calibration", apply: MigrateTrafficCalibration},
	{version: 22, name: "metric_erasure", apply: MigrateMetricErasure},
	{version: 23, name: "fleet_status_summary", apply: MigrateFleetSummary},
	{version: 24, name: "application_data_lifecycle", apply: MigrateApplicationDataLifecycle},
	{version: 25, name: "dashboard_collaboration", apply: applyDashboardCollaboration},
	{version: 26, name: "agent_discovery", apply: applyAgentDiscovery},
	{version: 27, name: "dashboard_versions", apply: applyDashboardVersions},
	{version: 28, name: "agent_task_output", apply: applyAgentTaskOutput},
	{version: 29, name: "agent_release_retention", apply: applyAgentReleaseRetention},
	{version: 30, name: "accelerator_public_visibility", apply: applyAcceleratorPublicVisibility},
}

// applyThemePackages 引入主题包、激活历史、市场源与站点活动主题持久化。
func applyThemePackages(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS theme_packages (
			id TEXT PRIMARY KEY,
			theme_id TEXT NOT NULL CHECK(length(theme_id) BETWEEN 3 AND 64),
			name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
			version TEXT NOT NULL CHECK(length(version) BETWEEN 1 AND 64),
			author TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			license TEXT NOT NULL DEFAULT '',
			runtime TEXT NOT NULL CHECK(runtime IN ('declarative', 'sandboxed')),
			manifest_schema INTEGER NOT NULL DEFAULT 1,
			minimum_beat_version TEXT NOT NULL DEFAULT '',
			maximum_beat_version TEXT,
			archive_digest TEXT NOT NULL UNIQUE CHECK(length(archive_digest) = 64),
			archive_bytes INTEGER NOT NULL CHECK(archive_bytes BETWEEN 0 AND 26214400),
			content_bytes INTEGER NOT NULL CHECK(content_bytes BETWEEN 0 AND 67108864),
			manifest_json TEXT NOT NULL,
			configuration_json TEXT NOT NULL DEFAULT '{}',
			source_type TEXT NOT NULL CHECK(source_type IN ('upload', 'market')),
			source_id TEXT,
			state TEXT NOT NULL CHECK(state IN ('installed', 'quarantined', 'deleted')),
			validated_at DATETIME NOT NULL,
			creator_id TEXT NOT NULL,
			last_error_category TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(theme_id, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_theme_packages_theme ON theme_packages(theme_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS theme_activations (
			id TEXT PRIMARY KEY,
			package_id TEXT,
			previous_package_id TEXT,
			actor_id TEXT NOT NULL,
			reason TEXT NOT NULL CHECK(reason IN ('activate', 'rollback', 'fallback')),
			activated_at DATETIME NOT NULL,
			deactivated_at DATETIME,
			health_state TEXT NOT NULL CHECK(health_state IN ('healthy', 'unhealthy')),
			last_failure_category TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(package_id) REFERENCES theme_packages(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_theme_activations_current ON theme_activations(deactivated_at)`,
		`CREATE TABLE IF NOT EXISTS theme_market_sources (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
			catalog_url TEXT NOT NULL CHECK(length(catalog_url) BETWEEN 1 AND 2048),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			sort_order INTEGER NOT NULL DEFAULT 0,
			public_key TEXT NOT NULL CHECK(length(public_key) BETWEEN 32 AND 128),
			key_fingerprint TEXT NOT NULL,
			cache_etag TEXT NOT NULL DEFAULT '',
			cache_last_modified TEXT NOT NULL DEFAULT '',
			fetched_at DATETIME,
			expires_at DATETIME,
			catalog_digest TEXT NOT NULL DEFAULT '',
			last_status TEXT NOT NULL DEFAULT '',
			last_error_category TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{name: "active_theme_package_id", ddl: "ALTER TABLE site_settings ADD COLUMN active_theme_package_id TEXT"},
	} {
		var count int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM pragma_table_info('site_settings') WHERE name = ?", column.name,
		).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyOIDCIdentity 引入 OIDC 提供方、外部身份与登录/链接事务表，并为会话
// 增加认证方法与外部身份归因列。
func applyOIDCIdentity(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS admin_oidc_providers (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE CHECK(length(slug) BETWEEN 1 AND 100),
			display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),
			issuer_url TEXT NOT NULL UNIQUE CHECK(length(issuer_url) BETWEEN 1 AND 2048),
			client_id TEXT NOT NULL CHECK(length(client_id) BETWEEN 1 AND 1024),
			client_secret_encrypted BLOB,
			scopes_json TEXT NOT NULL CHECK(length(scopes_json) BETWEEN 1 AND 2048),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			configuration_version INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_oidc_providers_enabled ON admin_oidc_providers(enabled, sort_order)`,
		`CREATE TABLE IF NOT EXISTS admin_external_identities (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			subject TEXT NOT NULL CHECK(length(subject) BETWEEN 1 AND 1024),
			email TEXT NOT NULL DEFAULT '',
			email_verified INTEGER NOT NULL DEFAULT 0 CHECK(email_verified IN (0, 1)),
			preferred_username TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			last_login_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(provider_id) REFERENCES admin_oidc_providers(id) ON DELETE RESTRICT,
			FOREIGN KEY(user_id) REFERENCES admin_users(id) ON DELETE CASCADE,
			UNIQUE(provider_id, subject),
			UNIQUE(provider_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_external_identities_user ON admin_external_identities(user_id)`,
		`CREATE TABLE IF NOT EXISTS admin_oidc_transactions (
			id TEXT PRIMARY KEY,
			state_hash BLOB NOT NULL UNIQUE,
			browser_binding_hash BLOB NOT NULL,
			nonce_hash BLOB NOT NULL,
			code_verifier_encrypted BLOB NOT NULL,
			provider_id TEXT NOT NULL,
			provider_configuration_version INTEGER NOT NULL,
			purpose TEXT NOT NULL CHECK(purpose IN ('login', 'link')),
			user_id TEXT,
			redirect_uri TEXT NOT NULL CHECK(length(redirect_uri) BETWEEN 1 AND 2048),
			return_path TEXT NOT NULL CHECK(length(return_path) BETWEEN 1 AND 512),
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			consumed_at DATETIME,
			FOREIGN KEY(provider_id) REFERENCES admin_oidc_providers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_oidc_transactions_expiry ON admin_oidc_transactions(expires_at)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{name: "authentication_method", ddl: "ALTER TABLE admin_sessions ADD COLUMN authentication_method TEXT NOT NULL DEFAULT 'local'"},
		{name: "external_identity_id", ddl: "ALTER TABLE admin_sessions ADD COLUMN external_identity_id TEXT"},
	} {
		var count int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM pragma_table_info('admin_sessions') WHERE name = ?", column.name,
		).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyAlertEventAcknowledgement(ctx context.Context, db schemaExecutor) error {
	// Add the acknowledged_at column if it does not already exist.
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('alert_events') WHERE name = 'acknowledged_at'",
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err := db.ExecContext(ctx,
			"ALTER TABLE alert_events ADD COLUMN acknowledged_at DATETIME",
		); err != nil {
			return err
		}
	}
	return nil
}

func applyCustomMetricTasks(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS custom_metrics (
			id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE CHECK(length(name) BETWEEN 1 AND 100),
			shell_command TEXT NOT NULL CHECK(length(CAST(shell_command AS BLOB)) BETWEEN 1 AND 2048),
			interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 5 AND 86400),
			description TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS custom_metric_nodes (
			metric_id TEXT NOT NULL, node_id TEXT NOT NULL, PRIMARY KEY(metric_id, node_id),
			FOREIGN KEY(metric_id) REFERENCES custom_metrics(id) ON DELETE CASCADE,
			FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_metric_nodes_node ON custom_metric_nodes(node_id, metric_id)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func applyAgentTaskChannel(ctx context.Context, db schemaExecutor) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS agent_tasks (
			id TEXT PRIMARY KEY, node_id TEXT NOT NULL, type TEXT NOT NULL,
			payload TEXT NOT NULL DEFAULT '', status TEXT NOT NULL CHECK(status IN ('pending', 'done', 'failed')),
			created_at DATETIME NOT NULL, attempted_at DATETIME, completed_at DATETIME,
			FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_tasks_pending ON agent_tasks(node_id, status)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	return applyMigrationsObserved(ctx, db, nil)
}

// applyMigrationsObserved 应用全部规范迁移，并向可选观察者报告有界阶段。
func applyMigrationsObserved(ctx context.Context, db *sql.DB, observer MigrationObserver) (resultErr error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL
	)`); err != nil {
		resultErr = migrationFail(MigrationFailureApply, fmt.Errorf("create migration ledger: %w", err))
		notifyFailure(observer, resultErr)
		return resultErr
	}
	source, err := currentMigrationVersion(ctx, db)
	if err != nil {
		notifyFailure(observer, err)
		return err
	}
	if source > currentSchemaVersion {
		resultErr = migrationFail(MigrationFailureFutureSchema,
			fmt.Errorf("database schema version %d is newer than supported version %d", source, currentSchemaVersion))
		notifyFailure(observer, resultErr)
		return resultErr
	}
	if observer != nil {
		observer.MigrationStarted(source)
	}
	for _, migration := range sqliteMigrations {
		if err := applySingleMigration(ctx, db, migration, observer); err != nil {
			notifyFailure(observer, err)
			return err
		}
	}
	if err := NewMetricCatalogStore(db).Reconcile(ctx); err != nil {
		resultErr = migrationFail(MigrationFailureApply, err)
		notifyFailure(observer, resultErr)
		return resultErr
	}
	if observer != nil {
		observer.MigrationCompleted(currentSchemaVersion)
	}
	return nil
}

func currentMigrationVersion(ctx context.Context, db *sql.DB) (int, error) {
	var maximum sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maximum); err != nil {
		return 0, migrationFail(MigrationFailureApply, fmt.Errorf("query migration version: %w", err))
	}
	if !maximum.Valid {
		return 0, nil
	}
	return int(maximum.Int64), nil
}

func applySingleMigration(ctx context.Context, db *sql.DB, migration sqliteMigration, observer MigrationObserver) error {
	applied, err := migrationApplied(ctx, db, migration.version)
	if err != nil {
		return migrationFail(MigrationFailureApply, err)
	}
	if applied {
		return nil
	}
	if err := applyMigration(ctx, db, migration); err != nil {
		code := MigrationFailureCode(err)
		if code == MigrationFailureUnknown {
			code = MigrationFailureApply
		}
		return migrationFail(code, err)
	}
	if observer != nil {
		observer.MigrationApplied(Migration{Version: migration.version, Name: migration.name}, migration.version)
	}
	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("query migration %d: %w", version, err)
	}
	return count == 1, nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration sqliteMigration) error {
	// 两个实例同时启动时，迁移事务的写锁可能被对方占着；这里沿用写入路径的
	// busy 重试，而不是把一次 SQLITE_BUSY 直接升级成启动失败。
	tx, err := beginWriteTx(ctx, db)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", migration.version, err)
	}
	defer func() { _ = tx.Rollback() }()
	var applied int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.version,
	).Scan(&applied); err != nil {
		return fmt.Errorf("recheck migration %d: %w", migration.version, err)
	}
	if applied == 1 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration recheck %d: %w", migration.version, err)
		}
		return nil
	}
	if err := migration.apply(ctx, tx); err != nil {
		return fmt.Errorf("apply migration %d %s: %w", migration.version, migration.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		migration.version, migration.name, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("record migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", migration.version, err)
	}
	return nil
}

func applyApplicationSchema(ctx context.Context, db schemaExecutor) error {
	if err := createTables(ctx, db); err != nil {
		return err
	}
	if err := ensureNodeColumns(ctx, db); err != nil {
		return fmt.Errorf("ensure node columns: %w", err)
	}
	if err := ensureDefaultGroup(ctx, db); err != nil {
		return fmt.Errorf("ensure default group: %w", err)
	}
	if err := ensureNodeIndexes(ctx, db); err != nil {
		return fmt.Errorf("ensure node indexes: %w", err)
	}
	return nil
}
