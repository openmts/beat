package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/beat/backend/internal/alerteval"
)

func TestAdvancedRuleListRejectsCapacityAndCorruptMetadata(t *testing.T) {
	for _, scenario := range []string{"capacity", "missing rules", "invalid scalar", "missing scope", "missing incidents"} {
		t.Run(scenario, func(t *testing.T) {
			sqlite, rules, change := evaluationRuleFixture(t)
			rule, err := rules.Create(t.Context(), change)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "capacity" {
				for index := range alerteval.MaximumRules {
					copy := *rule
					copy.ID = fmt.Sprintf("rule-%d", index)
					values := advancedAlertRuleValues(copy)
					if _, err := sqlite.DB.Exec("INSERT INTO alert_rules("+advancedAlertRuleColumns+") VALUES("+sqlPlaceholders(len(values))+")", values...); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				statement := map[string]string{"missing rules": "DROP TABLE alert_rules", "invalid scalar": "UPDATE alert_rules SET duration='broken'", "missing scope": "UPDATE alert_rules SET scope_kind='nodes'; DROP TABLE alert_rule_nodes", "missing incidents": "DROP TABLE alert_events"}[scenario]
				if _, err := sqlite.DB.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := rules.List(t.Context()); err == nil || scenario == "capacity" && !errors.Is(err, alerteval.ErrCapacity) {
				t.Fatal(scenario, err)
			}
		})
	}
}

func TestAdvancedRuleWriteRejectsStaleAndBrokenTransactions(t *testing.T) {
	sqlite, rules, change := evaluationRuleFixture(t)
	rule, err := rules.Create(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	rule.Revision++
	if err := persistAdvancedAlertRule(t.Context(), tx, *rule); err != nil {
		t.Fatal(err)
	}
	if err := persistAdvancedAlertRule(t.Context(), tx, *rule); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := tx.Exec("DROP TABLE alert_rules"); err != nil {
		t.Fatal(err)
	}
	if err := persistAdvancedAlertRule(t.Context(), tx, *rule); err == nil {
		t.Fatal("broken transaction accepted")
	}
}
