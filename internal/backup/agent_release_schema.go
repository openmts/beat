package backup

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const archivedAgentTableNames = `('agent_release_signature_policies','agent_release_signature_state','agent_release_trust','agent_releases','agent_release_artifacts','agent_rollouts','agent_rollout_targets','agent_rollout_attempts','agent_rollout_events')`

const agentReleaseMigrationVersion = 15

type archivedAgentTable struct{ name, columns, primary string }

var archivedAgentTables = []archivedAgentTable{
	{"agent_release_signature_policies", "revision:TEXT root_sha256:TEXT policy_json:BLOB created_at:DATETIME", "revision"},
	{"agent_release_signature_state", "id:INTEGER policy_revision:TEXT updated_at:DATETIME", "id"},
	{"agent_release_trust", "key_id:TEXT public_key:BLOB revision:INTEGER source_key:TEXT not_before:DATETIME expires_at:DATETIME revoked_at:DATETIME created_at:DATETIME", "key_id"},
	{"agent_releases", "id:TEXT version:TEXT channel:TEXT signing_key_id:TEXT payload:BLOB signature:BLOB sigstore_bundle:BLOB expires_at:DATETIME trust_revision:INTEGER signature_policy_revision:TEXT verified:INTEGER revoked_at:DATETIME created_by:TEXT created_at:DATETIME", "id"},
	{"agent_release_artifacts", "release_id:TEXT artifact_id:TEXT digest:TEXT size:INTEGER state:TEXT updated_at:DATETIME", "release_id artifact_id"},
	{"agent_rollouts", "action:TEXT id:TEXT release_id:TEXT name:TEXT state:TEXT reason:TEXT policy_json:TEXT current_wave:INTEGER total_waves:INTEGER created_by:TEXT created_at:DATETIME updated_at:DATETIME", "id"},
	{"agent_rollout_targets", "id:TEXT rollout_id:TEXT node_id:TEXT node_snapshot_id:TEXT node_name:TEXT source_version:TEXT os:TEXT arch:TEXT wave:INTEGER state:TEXT reason:TEXT current_attempt_id:TEXT artifact_id:TEXT nonce_hash:BLOB lease_expires_at:DATETIME healthy_since:DATETIME last_report_at:DATETIME observation_json:TEXT updated_at:DATETIME", "id"},
	{"agent_rollout_attempts", "id:TEXT target_id:TEXT nonce_hash:BLOB state:TEXT observation_json:TEXT created_at:DATETIME ended_at:DATETIME", "id"},
	{"agent_rollout_events", "id:TEXT rollout_id:TEXT target_id:TEXT kind:TEXT reason:TEXT created_at:DATETIME", "id"},
}

var errAgentReleaseArchive = errors.New("agent release archive metadata or schema is invalid")

func archivedAgentSchema(ctx context.Context, database *sql.DB) (bool, error) {
	var objects, tables int
	query := "SELECT COUNT(*),COALESCE(SUM(type='table' AND name IN " + archivedAgentTableNames + "),0) FROM sqlite_master WHERE lower(name) IN " + archivedAgentTableNames
	if err := database.QueryRowContext(ctx, query).Scan(&objects, &tables); err != nil {
		return false, err
	}
	if objects == 0 {
		return false, validateLegacyAgentSchema(ctx, database)
	}
	if objects != len(archivedAgentTables) || tables != objects {
		return false, errAgentReleaseArchive
	}
	for _, table := range archivedAgentTables {
		if err := validateAgentTable(ctx, database, table); err != nil {
			return false, err
		}
	}
	var trigger int
	query = "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='trigger' AND tbl_name IN " + archivedAgentTableNames + ")"
	if err := database.QueryRowContext(ctx, query).Scan(&trigger); err != nil {
		return false, err
	}
	if trigger != 0 {
		return false, errAgentReleaseArchive
	}
	return true, nil
}

func validateLegacyAgentSchema(ctx context.Context, database *sql.DB) error {
	var kind, name string
	err := database.QueryRowContext(ctx, "SELECT type,name FROM sqlite_master WHERE lower(name)='schema_migrations'").Scan(&kind, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if kind != "table" || name != "schema_migrations" {
		return errAgentReleaseArchive
	}
	var version int
	if err = database.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	if version >= agentReleaseMigrationVersion {
		return errAgentReleaseArchive
	}
	return nil
}

func validateAgentTable(ctx context.Context, database *sql.DB, table archivedAgentTable) (resultErr error) {
	rows, err := database.QueryContext(ctx, "SELECT name,type,pk,hidden FROM pragma_table_xinfo(?)", table.name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	types, keys := agentTableColumns(table)
	seen := 0
	for rows.Next() {
		var name, kind string
		var primary, hidden int
		if err = rows.Scan(&name, &kind, &primary, &hidden); err != nil {
			return err
		}
		if seen >= len(types) || !validAgentColumn(agentColumn{name, kind, primary, hidden}, types, keys) {
			return errAgentReleaseArchive
		}
		seen++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if seen != len(types) {
		return errAgentReleaseArchive
	}
	return nil
}

func agentTableColumns(table archivedAgentTable) (map[string]string, map[string]int) {
	types := map[string]string{}
	keys := map[string]int{}
	for _, definition := range strings.Fields(table.columns) {
		name, kind, _ := strings.Cut(definition, ":")
		types[name] = kind
	}
	for index, name := range strings.Fields(table.primary) {
		keys[name] = index + 1
	}
	return types, keys
}

type agentColumn struct {
	name, kind      string
	primary, hidden int
}

func validAgentColumn(column agentColumn, types map[string]string, keys map[string]int) bool {
	expected, exists := types[column.name]
	return exists && column.hidden == 0 && expected == strings.ToUpper(column.kind) && keys[column.name] == column.primary
}
