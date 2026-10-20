package store

import (
	"bytes"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

func TestAcceleratorAlertMigrationPreservesV12StateAndLease(t *testing.T) {
	sqlite := preAlertEvaluationFixture(t)
	migrateAlertEvaluationFixture(t, sqlite)
	_, err := sqlite.DB.Exec(`INSERT INTO alert_evaluation_states(rule_id,node_id,rule_revision,state_version,phase,evaluation_status,reason_code,
 trigger_elapsed_ms,recovery_elapsed_ms,pending_since,recovery_since,last_evaluated_at,last_good_at,last_evidence_at_ns,last_evidence_observed_at,last_evidence_id,last_run_id,
 active_event_id,active_event_status,health_epoch_id,health_status,health_started_at,health_status_since,health_last_observed_at,health_elapsed_ms,health_seen_mask,health_notified_mask,health_suspended,
 next_due_at,updated_at,lease_token_hash,lease_expires_at,worker_id)
 VALUES('rule','node',3,7,'firing','ok','retained',3000,5000,100,200,300,400,987654321000,500,'sample','run','incident','acknowledged',
 '12345678-1234-1234-1234-123456789012','no_data',100,200,300,4000,3,1,1,600,700,?,800,'worker')`, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	before := acceleratorV12StateRow(t, sqlite.DB)
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := migrateAcceleratorAlerts(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after := acceleratorV12StateRow(t, sqlite.DB)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("v12 state or lease changed: before=%v after=%v", before, after)
	}
	var device string
	if err := sqlite.DB.QueryRow("SELECT device_id FROM alert_evaluation_states").Scan(&device); err != nil || device != "" {
		t.Fatal("legacy state not mapped to scalar dimension", err)
	}
	if _, err := sqlite.DB.Exec(`INSERT INTO alert_evaluation_states(rule_id,node_id,device_id,rule_revision,state_version,phase,evaluation_status,updated_at) VALUES('rule','node',?,1,1,'normal','no_data',0)`, strings.Repeat("a", 32)); err != nil {
		t.Fatal("GPU dimension collides with old scalar target", err)
	}
	if err := migrateAcceleratorAlerts(t.Context(), sqlite.DB); err == nil {
		t.Fatal("reapplying v14 to an incompatible source was silently accepted")
	}
}

func acceleratorV12StateRow(t *testing.T, db *sql.DB) []any {
	t.Helper()
	values := make([]any, len(strings.Split(acceleratorAlertV12StateColumns, ",")))
	destinations := make([]any, len(values))
	for i := range values {
		destinations[i] = &values[i]
	}
	if err := db.QueryRow("SELECT " + acceleratorAlertV12StateColumns + " FROM alert_evaluation_states WHERE rule_id='rule' AND node_id='node'").Scan(destinations...); err != nil {
		t.Fatal(err)
	}
	return values
}
