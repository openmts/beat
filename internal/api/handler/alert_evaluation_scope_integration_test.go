package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

func TestAlertScopeCapacityRejectionReachesNodeAndGroupHTTP(t *testing.T) {
	sqlite := setupTestDB(t)
	var defaultID string
	if err := sqlite.DB.QueryRow("SELECT id FROM groups WHERE is_default=1").Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec(`WITH RECURSIVE seeds(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seeds WHERE n<10000)
 INSERT INTO nodes(id,name,alias,ssh_public_key,host,port,group_id,created_at,updated_at,last_seen) SELECT 'seed-'||n,'seed-'||n,'','','192.0.2.1',22,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM seeds`, defaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec(`INSERT INTO alert_rules(id,name,metric,operator,threshold,scope_kind,created_at,updated_at) VALUES('capacity','Capacity','cpu','gt',80,'groups',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("INSERT INTO alert_rule_groups(rule_id,group_id) VALUES('capacity',?)", defaultID); err != nil {
		t.Fatal(err)
	}
	groups := store.NewGroupStore(sqlite.DB)
	outside, err := groups.CreateGroup(t.Context(), "Outside")
	if err != nil {
		t.Fatal(err)
	}
	nodes := store.NewNodeStore(sqlite.DB)
	node, err := nodes.CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "outside", Host: "192.0.2.3", Port: 22, GroupID: outside.ID}, store.AgentCredential{CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	h := NewNodeHandler(nodes, nil)
	cases := []struct {
		handle   http.HandlerFunc
		body, id string
	}{
		{h.HandleCreateManagedNode, `{"name":"overflow","host":"192.0.2.2","port":22,"server_url":"https://beat.example"}`, ""},
		{h.HandleUpdateNode, `{"group_id":"` + defaultID + `"}`, node.ID},
		{NewGroupHandler(groups).HandleDeleteGroup, "", outside.ID},
	}
	for _, test := range cases {
		request := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		request.SetPathValue("id", test.id)
		response := httptest.NewRecorder()
		test.handle(response, request)
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "30" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
