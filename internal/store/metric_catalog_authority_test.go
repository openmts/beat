package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMetricCatalogPolicyRechecksRevocationRoleAndRecentProofInTransaction(t *testing.T) {
	for _, mutation := range []string{"revoked", "demoted", "disabled", "idle expired", "absolute expired", "proof expired", "wrong user", "missing session"} {
		t.Run(mutation, func(t *testing.T) {
			sqlite := setupTestDB(t)
			catalog := NewMetricCatalogStore(sqlite.DB)
			definition, err := catalog.Get(t.Context(), "cpu")
			if err != nil {
				t.Fatal(err)
			}
			policy := metricPolicyFrom(definition)
			policy.Label = "Must not be saved"
			change := catalogPolicyWrite(t, catalog, policy)
			now := model.NowUTC()
			switch mutation {
			case "revoked":
				_, err = sqlite.DB.Exec("UPDATE admin_sessions SET revoked_at=? WHERE id=?", now, change.SessionID)
			case "demoted":
				_, err = sqlite.DB.Exec("UPDATE admin_users SET role='admin' WHERE id=?", change.Audit.ActorID)
			case "disabled":
				_, err = sqlite.DB.Exec("UPDATE admin_users SET enabled=0 WHERE id=?", change.Audit.ActorID)
			case "idle expired":
				_, err = sqlite.DB.Exec("UPDATE admin_sessions SET idle_expires_at=? WHERE id=?", now, change.SessionID)
			case "absolute expired":
				_, err = sqlite.DB.Exec("UPDATE admin_sessions SET absolute_expires_at=? WHERE id=?", now, change.SessionID)
			case "proof expired":
				_, err = sqlite.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=? WHERE id=?", now, change.SessionID)
			case "wrong user":
				change.Audit.ActorID = "different-user"
			case "missing session":
				change.SessionID = ""
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.UpdatePolicy(t.Context(), "cpu", change); !errors.Is(err, ErrMetricCatalogForbidden) {
				t.Fatalf("stale authority accepted: %v", err)
			}
			after, err := catalog.Get(t.Context(), "cpu")
			if err != nil || after.Label != definition.Label || after.Revision != definition.Revision {
				t.Fatal("rejected authority changed policy")
			}
			var jobs int
			if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM metric_retention_jobs").Scan(&jobs); err != nil || jobs != 0 {
				t.Fatal("rejected authority created work")
			}
		})
	}
}
