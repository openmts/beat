package store

import (
	"context"
	"strings"
)

// migrateAcceleratorAlerts 由v14统一事务调用，保留v12状态与租约且默认属于整机维度。
func migrateAcceleratorAlerts(ctx context.Context, database schemaExecutor) error {
	device := ` TEXT NOT NULL DEFAULT '' CHECK(device_id='' OR (length(device_id)=32 AND device_id NOT GLOB '*[^0-9a-f]*'))`
	rules := `ALTER TABLE alert_rules ADD COLUMN device_id TEXT NOT NULL DEFAULT '' CHECK(device_id IN ('','*') OR (length(device_id)=32 AND device_id NOT GLOB '*[^0-9a-f]*'))`
	if err := executeSchemaStatements(ctx, database, []string{rules,
		"ALTER TABLE alert_events ADD COLUMN device_id" + device,
		"ALTER TABLE alert_events ADD COLUMN device_name TEXT NOT NULL DEFAULT '' CHECK(length(CAST(device_name AS BLOB))<=256)",
		"ALTER TABLE alert_events ADD COLUMN resolution_reason TEXT NOT NULL DEFAULT '' CHECK(length(resolution_reason)<=64)",
		"ALTER TABLE alert_evaluation_events ADD COLUMN device_id" + device,
	}); err != nil {
		return err
	}
	ddl := strings.Replace(alertEvaluationStateSchema[0], "CREATE TABLE alert_evaluation_states(", "CREATE TABLE alert_evaluation_states_v14(", 1)
	ddl = strings.Replace(ddl, "rule_id TEXT NOT NULL,node_id TEXT NOT NULL,", "rule_id TEXT NOT NULL,node_id TEXT NOT NULL,device_id"+device+",", 1)
	ddl = strings.Replace(ddl, "PRIMARY KEY(rule_id,node_id)", "PRIMARY KEY(rule_id,node_id,device_id)", 1)
	columns := acceleratorAlertV12StateColumns
	return executeSchemaStatements(ctx, database, []string{ddl,
		"INSERT INTO alert_evaluation_states_v14(" + columns + ") SELECT " + columns + " FROM alert_evaluation_states",
		"DROP TABLE alert_evaluation_states", "ALTER TABLE alert_evaluation_states_v14 RENAME TO alert_evaluation_states",
		"CREATE INDEX alert_evaluation_due ON alert_evaluation_states(next_due_at,rule_id,node_id,device_id)",
		"CREATE INDEX alert_evaluation_status ON alert_evaluation_states(evaluation_status,updated_at,rule_id,node_id,device_id)",
		"CREATE INDEX alert_evaluation_incident ON alert_evaluation_states(active_event_id)",
		"DROP INDEX alert_evaluation_event_pair", "CREATE INDEX alert_evaluation_event_pair ON alert_evaluation_events(rule_id,node_id,device_id,occurred_at DESC,id)",
		"CREATE INDEX alert_event_device_active ON alert_events(rule_id,node_id,device_id,status)",
	})
}

// 固定v12已存在列，不能跟随当前runtime state字段扩展。
const acceleratorAlertV12StateColumns = `rule_id,node_id,rule_revision,state_version,phase,evaluation_status,reason_code,
 trigger_elapsed_ms,recovery_elapsed_ms,pending_since,recovery_since,last_evaluated_at,last_good_at,
 last_evidence_at_ns,last_evidence_observed_at,last_evidence_id,last_run_id,active_event_id,active_event_status,
 health_epoch_id,health_status,health_started_at,health_status_since,health_last_observed_at,health_elapsed_ms,
 health_seen_mask,health_notified_mask,health_suspended,next_due_at,updated_at,lease_token_hash,lease_expires_at,worker_id`
