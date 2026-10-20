package store

import (
	"context"
	"fmt"
)

// MigrateEdgeAccessPolicy 把站点地址布尔开关升级为三态展示模式，并登记有界的
// 公共交互聚合遥测策略。
//
// 归一化外部地址与来源列表永远是部署配置，不进入 SQLite；这里只保存策略本身。
// 旧值的语义被完整保留：show_ip_addresses=1 迁移为 full，=0 迁移为 hidden。
// 全新实例的基线行写入 0，因此在没有任何历史值的情况下默认 hidden。
func MigrateEdgeAccessPolicy(ctx context.Context, db schemaExecutor) error {
	statements := []string{
		`ALTER TABLE site_settings RENAME TO site_settings_v19`,
		`CREATE TABLE site_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1), site_title TEXT NOT NULL CHECK(length(site_title) BETWEEN 1 AND 80),
			site_description TEXT NOT NULL CHECK(length(site_description) <= 240),
			logo_url TEXT NOT NULL CHECK(length(CAST(logo_url AS BLOB)) <= 2048),
			favicon_url TEXT NOT NULL CHECK(length(CAST(favicon_url AS BLOB)) <= 2048),
			default_theme TEXT NOT NULL CHECK(default_theme IN ('system', 'light', 'dark')),
			public_address_mode TEXT NOT NULL DEFAULT 'hidden' CHECK(public_address_mode IN ('hidden', 'masked', 'full')),
			show_network_quality INTEGER NOT NULL CHECK(show_network_quality IN (0, 1)),
			active_theme_package_id TEXT, updated_at DATETIME NOT NULL
		)`,
		`INSERT INTO site_settings (
			id, site_title, site_description, logo_url, favicon_url, default_theme,
			public_address_mode, show_network_quality, active_theme_package_id, updated_at
		) SELECT id, site_title, site_description, logo_url, favicon_url, default_theme,
			CASE WHEN show_ip_addresses = 1 THEN 'full' ELSE 'hidden' END, show_network_quality, active_theme_package_id, updated_at
		FROM site_settings_v19`,
		`DROP TABLE site_settings_v19`,
		`CREATE TABLE IF NOT EXISTS public_telemetry_policy (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0, 1)),
			allowed_events_json TEXT NOT NULL CHECK(length(allowed_events_json) <= 512),
			sample_ceiling INTEGER NOT NULL CHECK(sample_ceiling BETWEEN 1 AND 100000),
			retention_days INTEGER NOT NULL CHECK(retention_days BETWEEN 1 AND 3650),
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision >= 1),
			updated_by TEXT NOT NULL DEFAULT '' CHECK(length(updated_by) <= 64),
			updated_at DATETIME NOT NULL
		)`,
		`INSERT OR IGNORE INTO public_telemetry_policy (
			id, enabled, allowed_events_json, sample_ceiling, retention_days, revision, updated_by, updated_at
		) VALUES (1, 0, '["page_view","node_open","range_change","wallboard_open"]', 1000, 30, 1, '', CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply edge access policy schema: %w", err)
		}
	}
	return nil
}
