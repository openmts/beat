package agentrollouthttp

import (
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
)

func TestOwnerStatusAndObservationTimeJSON(t *testing.T) {
	f := newFixture(t)
	rollout, _ := f.rollout(t, 1)
	status := result[agentrollout.Status](t, f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-rollout-status"}))
	if !status.Enabled || !status.Configured || !status.Ready || status.Reason != "ready" {
		t.Fatal(status)
	}
	page := f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-rollouts/" + rollout.ID + "/targets"})
	for _, field := range []string{`"healthy_since":null`, `"last_report_at":null`, `"lease_expires_at":null`, `"server_time":"` + f.now.Format(time.RFC3339) + `"`} {
		if !strings.Contains(page.Body.String(), field) {
			t.Fatal(field, page.Body.String())
		}
	}
	detail := f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-rollouts/" + rollout.ID})
	if !strings.Contains(detail.Body.String(), `"server_time"`) {
		t.Fatal(detail.Body.String())
	}
	disabled, err := agentrollout.New(agentrollout.Config{Database: f.db.DB, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.s = disabled
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	status = result[agentrollout.Status](t, f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-rollout-status"}))
	if status.Enabled || status.Ready || status.Reason != "disabled" {
		t.Fatal(status)
	}
	denied := f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-rollout-status", anonymous: true})
	if denied.Code != 403 {
		t.Fatal(denied.Code)
	}
}
