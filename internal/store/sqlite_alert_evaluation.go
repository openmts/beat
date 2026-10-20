package store

import "context"

// MigrateAlertEvaluation 仅提供 v12 DDL；登记顺序由主迁移账本在 v11 就绪后决定。
func MigrateAlertEvaluation(ctx context.Context, database schemaExecutor) error {
	if err := executeSchemaStatements(ctx, database, alertEvaluationRuleSchema); err != nil {
		return err
	}
	if err := executeSchemaStatements(ctx, database, alertEvaluationStateSchema); err != nil {
		return err
	}
	if err := executeSchemaStatements(ctx, database, alertEvaluationEventSchema); err != nil {
		return err
	}
	return executeSchemaStatements(ctx, database, reportPreparationSchema)
}

var alertEvaluationRuleSchema = []string{
	`ALTER TABLE alert_rules ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0)`,
	`ALTER TABLE alert_rules ADD COLUMN deleted_at INTEGER`,
	`ALTER TABLE alert_rules ADD COLUMN scope_kind TEXT NOT NULL DEFAULT 'all' CHECK(scope_kind IN ('all','nodes','groups'))`,
	`ALTER TABLE alert_rules ADD COLUMN window_seconds INTEGER NOT NULL DEFAULT 0 CHECK(window_seconds=0 OR window_seconds BETWEEN 10 AND 86400)`,
	`ALTER TABLE alert_rules ADD COLUMN aggregation TEXT NOT NULL DEFAULT 'last' CHECK(aggregation IN ('last','avg','min','max','sum','rate'))`,
	`ALTER TABLE alert_rules ADD COLUMN recovery_threshold REAL CHECK(recovery_threshold IS NULL OR abs(recovery_threshold)<=1.7976931348623157e308)`,
	`ALTER TABLE alert_rules ADD COLUMN recovery_duration INTEGER NOT NULL DEFAULT 0 CHECK(recovery_duration BETWEEN 0 AND 86400)`,
	`ALTER TABLE alert_rules ADD COLUMN no_data_policy TEXT NOT NULL DEFAULT 'keep' CHECK(no_data_policy IN ('keep','notify'))`,
	`ALTER TABLE alert_rules ADD COLUMN no_data_delay_seconds INTEGER NOT NULL DEFAULT 300 CHECK(no_data_delay_seconds BETWEEN 0 AND 86400)`,
	`ALTER TABLE alert_rules ADD COLUMN error_policy TEXT NOT NULL DEFAULT 'keep' CHECK(error_policy IN ('keep','notify'))`,
	`ALTER TABLE alert_rules ADD COLUMN error_delay_seconds INTEGER NOT NULL DEFAULT 60 CHECK(error_delay_seconds BETWEEN 0 AND 86400)`,
	`ALTER TABLE alert_events ADD COLUMN rule_revision INTEGER NOT NULL DEFAULT 1 CHECK(rule_revision>0)`,
	`CREATE TABLE alert_rule_nodes(rule_id TEXT NOT NULL,node_id TEXT NOT NULL,PRIMARY KEY(rule_id,node_id),
		FOREIGN KEY(rule_id) REFERENCES alert_rules(id) ON DELETE RESTRICT)`,
	`CREATE TABLE alert_rule_groups(rule_id TEXT NOT NULL,group_id TEXT NOT NULL,PRIMARY KEY(rule_id,group_id),
		FOREIGN KEY(rule_id) REFERENCES alert_rules(id) ON DELETE RESTRICT)`,
	`CREATE INDEX alert_rule_node_lookup ON alert_rule_nodes(node_id,rule_id)`,
	`CREATE INDEX alert_rule_group_lookup ON alert_rule_groups(group_id,rule_id)`,
	`CREATE INDEX alert_evaluation_node_group ON nodes(group_id)`,
	`CREATE INDEX alert_rule_policy_lookup ON alert_rules(deleted_at,enabled,id)`,
}
