package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const groupScopeCountQuery = `SELECT COUNT(*) FROM nodes n WHERE n.group_id IN (SELECT group_id FROM alert_rule_groups b WHERE b.rule_id=?)`

func TestAlertGroupScopeCapacityUsesSelectiveIndex(t *testing.T) {
	sqlite := setupTestDB(t)
	selected, err := NewGroupStore(sqlite.DB).CreateGroup(t.Context(), "Selected")
	if err != nil {
		t.Fatal(err)
	}
	var defaultID string
	if err := sqlite.DB.QueryRow("SELECT id FROM groups WHERE is_default=1").Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec(`WITH RECURSIVE seeds(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seeds WHERE n<10000)
 INSERT INTO nodes(id,name,alias,ssh_public_key,host,port,group_id,created_at,updated_at,last_seen)
 SELECT 'group-plan-'||n,'group-plan-'||n,'','','192.0.2.1',22,CASE WHEN n<=100 THEN ? ELSE ? END,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM seeds`, selected.ID, defaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec(`INSERT INTO alert_rules(id,name,metric,operator,threshold,scope_kind,created_at,updated_at) VALUES('group-plan','Plan','cpu','gt',80,'groups',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("INSERT INTO alert_rule_groups(rule_id,group_id) VALUES('group-plan',?)", selected.ID); err != nil {
		t.Fatal(err)
	}
	plan := alertGroupQueryPlan(t, sqlite)
	started := time.Now()
	for range 25 {
		var count int
		if err := sqlite.DB.QueryRow(groupScopeCountQuery, "group-plan").Scan(&count); err != nil || count != 100 {
			t.Fatal(count, err)
		}
	}
	t.Logf("10,000 nodes / 100 selected, 25 counts: %s; plan: %s", time.Since(started), plan)
	if !strings.Contains(plan, "SEARCH n USING COVERING INDEX alert_evaluation_node_group") {
		t.Fatal("group scope scanned unrelated nodes", plan)
	}
}

func alertGroupQueryPlan(t *testing.T, sqlite *SQLiteStore) string {
	t.Helper()
	rows, err := sqlite.DB.Query("EXPLAIN QUERY PLAN "+groupScopeCountQuery, "group-plan")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	lines := []string{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%d %s", id, detail))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "; ")
}
