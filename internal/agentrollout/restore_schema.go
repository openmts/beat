package agentrollout

import (
	"context"
	"database/sql"
	"errors"
)

const rolloutMigrationVersion = 15

const rolloutRestoreTables = 9

// restoredSchemaPresent 允许旧归档缺少整套功能，拒绝部分表或冒充表的视图。
func restoredSchemaPresent(ctx context.Context, database *sql.DB) (bool, error) {
	var objects, tables int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(type='table' AND name=lower(name)),0) FROM sqlite_master
 WHERE lower(name) IN ('agent_release_signature_policies','agent_release_signature_state','agent_release_trust',
 'agent_releases','agent_release_artifacts','agent_rollouts','agent_rollout_targets','agent_rollout_attempts','agent_rollout_events')`).Scan(&objects, &tables)
	if err != nil {
		return false, err
	}
	if objects != 0 {
		if objects != rolloutRestoreTables || tables != rolloutRestoreTables {
			return false, ErrUnavailable
		}
		return true, nil
	}
	return false, validateLegacyRestoreSchema(ctx, database)
}

func validateLegacyRestoreSchema(ctx context.Context, database *sql.DB) error {
	var name, kind string
	err := database.QueryRowContext(ctx, "SELECT name,type FROM sqlite_master WHERE lower(name)='schema_migrations'").Scan(&name, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if name != "schema_migrations" || kind != "table" {
		return ErrUnavailable
	}
	var version int
	if err := database.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	if version >= rolloutMigrationVersion {
		return ErrUnavailable
	}
	return nil
}
