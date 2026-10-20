package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func seedAlertScopeCapacity(t *testing.T, sqlite *SQLiteStore, groups bool) string {
	t.Helper()
	var groupID string
	if err := sqlite.DB.QueryRow("SELECT id FROM groups WHERE is_default=1").Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec(`WITH RECURSIVE seeds(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seeds WHERE n<10000)
 INSERT INTO nodes(id,name,alias,ssh_public_key,host,port,group_id,created_at,updated_at,last_seen) SELECT 'seed-'||n,'seed-'||n,'','','192.0.2.1',22,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM seeds`, groupID); err != nil {
		t.Fatal(err)
	}
	kind := "all"
	if groups {
		kind = "groups"
	}
	if _, err := sqlite.DB.Exec(`INSERT INTO alert_rules(id,name,metric,operator,threshold,scope_kind,created_at,updated_at) VALUES('capacity','Capacity','cpu','gt',80,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, kind); err != nil {
		t.Fatal(err)
	}
	if groups {
		if _, err := sqlite.DB.Exec("INSERT INTO alert_rule_groups(rule_id,group_id) VALUES('capacity',?)", groupID); err != nil {
			t.Fatal(err)
		}
	}
	return groupID
}

func TestAlertScopeCapacityRejectsManagedAndHeartbeatCreationAtomically(t *testing.T) {
	sqlite := setupTestDB(t)
	seedAlertScopeCapacity(t, sqlite, false)
	nodes := NewNodeStore(sqlite.DB)
	if _, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "managed-overflow", Host: "192.0.2.2", Port: 22}, AgentCredential{CreatedAt: time.Now()}); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := nodes.UpsertNode(t.Context(), "heartbeat-overflow", "192.0.2.3", 22); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := nodes.UpsertNode(t.Context(), "seed-1", "192.0.2.4", 22); err != nil {
		t.Fatal("existing heartbeat blocked", err)
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != alerteval.MaximumPairs {
		t.Fatal(count, err)
	}
}

func TestAlertScopeCapacityRejectsGroupMoveAndGroupDeletion(t *testing.T) {
	sqlite := setupTestDB(t)
	target := seedAlertScopeCapacity(t, sqlite, true)
	group, err := NewGroupStore(sqlite.DB).CreateGroup(t.Context(), "Outside")
	if err != nil {
		t.Fatal(err)
	}
	nodes := NewNodeStore(sqlite.DB)
	node, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "outside", Host: "192.0.2.8", Port: 22, GroupID: group.ID}, AgentCredential{CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{GroupID: target, Alias: "must rollback"}); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	if err := NewGroupStore(sqlite.DB).DeleteGroup(t.Context(), group.ID); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	retained, err := nodes.GetNode(t.Context(), node.ID)
	if err != nil || retained.GroupID != group.ID || retained.Alias != "" {
		t.Fatal(retained, err)
	}
	if group, err := NewGroupStore(sqlite.DB).GetGroup(t.Context(), group.ID); err != nil || group == nil {
		t.Fatal(group, err)
	}
	if _, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{GroupID: group.ID, Alias: "allowed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("UPDATE alert_rules SET enabled=0"); err != nil {
		t.Fatal(err)
	}
	if updated, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{GroupID: target}); err != nil || updated.GroupID != target {
		t.Fatal(updated, err)
	}
}

func TestAlertScopeCapacityDoesNotConsumeEnrollmentCredential(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	seedAlertScopeCapacity(t, sqlite, false)
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM agent_enrollment_tokens WHERE consumed_at IS NOT NULL").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='agent.enrollment.consume' AND outcome=?", model.AuditOutcomeSuccess).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err := sqlite.DB.Exec("DELETE FROM nodes WHERE id='seed-1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err != nil {
		t.Fatal("rejected credential became unusable", err)
	}
}

func TestAlertScopeCapacitySerializesConcurrentAdmission(t *testing.T) {
	sqlite := setupTestDB(t)
	seedAlertScopeCapacity(t, sqlite, false)
	if _, err := sqlite.DB.Exec("DELETE FROM nodes WHERE id='seed-1'"); err != nil {
		t.Fatal(err)
	}
	nodes := NewNodeStore(sqlite.DB)
	results := make(chan error, 2)
	for index := range 2 {
		go func(index int) {
			_, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: fmt.Sprintf("candidate-%d", index), Host: "192.0.2.7", Port: 22}, AgentCredential{CreatedAt: time.Now()})
			results <- err
		}(index)
	}
	admitted, rejected := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			admitted++
		} else if errors.Is(err, alerteval.ErrCapacity) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if admitted != 1 || rejected != 1 {
		t.Fatal(admitted, rejected)
	}
}

func TestAlertScopeGuardRejectsStorageReadFailure(t *testing.T) {
	sqlite := setupTestDB(t)
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := guardAlertScopeMutation(ctx, tx); err == nil {
		t.Fatal("failed feature check bypassed capacity guard")
	}
}

func TestAlertScopeGuardFailsClosedForMissingRegisteredSchema(t *testing.T) {
	sqlite := setupTestDB(t)
	if _, err := sqlite.DB.Exec("DROP TABLE alert_rule_nodes"); err != nil {
		t.Fatal(err)
	}
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := guardAlertScopeMutation(t.Context(), tx); !errors.Is(err, alerteval.ErrUnavailable) {
		t.Fatal("missing registered schema bypassed guard", err)
	}
}
