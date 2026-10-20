package store

// 历史与活动状态刻意不对 node/rule 建级联外键，删除来源不能抹去事件证据。
var alertEvaluationStateSchema = []string{
	`CREATE TABLE alert_evaluation_states(
		rule_id TEXT NOT NULL,node_id TEXT NOT NULL,rule_revision INTEGER NOT NULL CHECK(rule_revision>0),
		state_version INTEGER NOT NULL CHECK(state_version>0),
		phase TEXT NOT NULL CHECK(phase IN ('normal','pending','firing','recovering')),
		evaluation_status TEXT NOT NULL CHECK(evaluation_status IN ('ok','no_data','error','disabled','out_of_scope')),
		reason_code TEXT NOT NULL DEFAULT '' CHECK(length(reason_code)<=64),
		trigger_elapsed_ms INTEGER NOT NULL DEFAULT 0 CHECK(trigger_elapsed_ms BETWEEN 0 AND 86400000),
		recovery_elapsed_ms INTEGER NOT NULL DEFAULT 0 CHECK(recovery_elapsed_ms BETWEEN 0 AND 86400000),
		pending_since INTEGER,recovery_since INTEGER,last_evaluated_at INTEGER,last_good_at INTEGER,
		last_evidence_at_ns INTEGER,last_evidence_observed_at INTEGER,
		last_evidence_id TEXT NOT NULL DEFAULT '' CHECK(length(last_evidence_id)<=256),
		last_run_id TEXT NOT NULL DEFAULT '' CHECK(length(last_run_id)<=36),
		active_event_id TEXT NOT NULL DEFAULT '',active_event_status TEXT NOT NULL DEFAULT ''
			CHECK(active_event_status IN ('','triggered','acknowledged')),
		health_epoch_id TEXT NOT NULL DEFAULT '' CHECK(length(health_epoch_id) IN (0,36)),
		health_status TEXT NOT NULL DEFAULT '' CHECK(health_status IN ('','no_data','error')),
		health_started_at INTEGER,health_status_since INTEGER,health_last_observed_at INTEGER,
		health_elapsed_ms INTEGER NOT NULL DEFAULT 0 CHECK(health_elapsed_ms BETWEEN 0 AND 86400000),
		health_seen_mask INTEGER NOT NULL DEFAULT 0 CHECK(health_seen_mask BETWEEN 0 AND 3),
		health_notified_mask INTEGER NOT NULL DEFAULT 0 CHECK(health_notified_mask BETWEEN 0 AND 3),
		health_suspended INTEGER NOT NULL DEFAULT 0 CHECK(health_suspended IN (0,1)),
		next_due_at INTEGER NOT NULL DEFAULT 0,updated_at INTEGER NOT NULL,
		lease_token_hash BLOB,lease_expires_at INTEGER,worker_id TEXT NOT NULL DEFAULT '' CHECK(length(worker_id)<=36),
		CHECK((lease_token_hash IS NULL AND lease_expires_at IS NULL AND worker_id='') OR
			(length(lease_token_hash)=32 AND lease_expires_at IS NOT NULL AND worker_id<>'')),
		PRIMARY KEY(rule_id,node_id)
	)`,
	`CREATE INDEX alert_evaluation_due ON alert_evaluation_states(next_due_at,rule_id,node_id)`,
	`CREATE INDEX alert_evaluation_status ON alert_evaluation_states(evaluation_status,updated_at,rule_id,node_id)`,
	`CREATE INDEX alert_evaluation_incident ON alert_evaluation_states(active_event_id)`,
	`INSERT INTO alert_evaluation_states(rule_id,node_id,rule_revision,state_version,phase,evaluation_status,
		active_event_id,active_event_status,updated_at)
		SELECT e.rule_id,e.node_id,COALESCE(r.revision,1),1,'firing',
		CASE WHEN r.id IS NULL OR r.enabled=0 THEN 'disabled' WHEN n.id IS NULL THEN 'out_of_scope' ELSE 'ok' END,
		e.id,e.status,COALESCE(CAST(strftime('%s',e.triggered_at) AS INTEGER)*1000,0)
		FROM alert_events e LEFT JOIN alert_rules r ON r.id=e.rule_id LEFT JOIN nodes n ON n.id=e.node_id
		WHERE e.status IN ('triggered','acknowledged')`,
}

var alertEvaluationEventSchema = []string{
	`CREATE TABLE alert_evaluation_events(
		id TEXT PRIMARY KEY CHECK(length(id)=36),rule_id TEXT NOT NULL,node_id TEXT NOT NULL,
		rule_revision INTEGER NOT NULL CHECK(rule_revision>0),epoch_id TEXT NOT NULL CHECK(length(epoch_id)=36),
		kind TEXT NOT NULL CHECK(kind IN ('no_data','error','recovered')),
		reason_code TEXT NOT NULL DEFAULT '' CHECK(length(reason_code)<=64),
		occurred_at INTEGER NOT NULL,notified_at INTEGER,closed_at INTEGER,
		source_key TEXT NOT NULL UNIQUE CHECK(length(source_key) BETWEEN 1 AND 512),
		UNIQUE(epoch_id,kind)
	)`,
	`CREATE INDEX alert_evaluation_event_history ON alert_evaluation_events(occurred_at DESC,id)`,
	`CREATE INDEX alert_evaluation_event_pair ON alert_evaluation_events(rule_id,node_id,occurred_at DESC,id)`,
	`CREATE INDEX alert_evaluation_event_epoch ON alert_evaluation_events(epoch_id,kind)`,
}
