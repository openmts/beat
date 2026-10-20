package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

func TestManualAlertTransitionsPersistEventAuditAndQueue(t *testing.T) {
	fixture := newManualEventFixture(t)
	for _, target := range []model.AlertStatus{model.AlertStatusAcknowledged, model.AlertStatusResolved} {
		response := fixture.request(t, target)
		if response.Code != http.StatusOK {
			t.Fatalf("transition: %d %s", response.Code, response.Body.String())
		}
		result := queuedEventResponse(t, response)
		if result.Status != target || result.Notification.State != "pending" {
			t.Fatalf("false delivery status: %+v", result)
		}
		stored, err := fixture.handler.alertEventStore.GetEventByID(t.Context(), fixture.event.ID)
		if err != nil || stored.Status != target || stored.AcknowledgedAt == nil {
			t.Fatalf("stored=%+v/%v", stored, err)
		}
		if target == model.AlertStatusResolved && stored.ResolvedAt == nil {
			t.Fatal("missing resolution time")
		}
		if fixture.queue.request.SourceKey != "alert:event:"+string(target) || fixture.queue.request.Data["event.status"] != string(target) {
			t.Fatal("incorrect queue source identity")
		}
		var count int
		if err := fixture.database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE resource_id=? AND action=? AND actor_id=?", fixture.event.ID, "alert."+string(target), fixture.principal.User.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("state transition was not audited")
		}
	}
}

func TestManualAlertRejectsInvalidStateAndMissingRecords(t *testing.T) {
	tests := []struct {
		name           string
		status, target model.AlertStatus
		missing        bool
		want           int
	}{
		{"ack twice", model.AlertStatusAcknowledged, model.AlertStatusAcknowledged, false, 409},
		{"ack resolved", model.AlertStatusResolved, model.AlertStatusAcknowledged, false, 409},
		{"resolve triggered", model.AlertStatusTriggered, model.AlertStatusResolved, false, 409},
		{"resolve twice", model.AlertStatusResolved, model.AlertStatusResolved, false, 409},
		{"missing ack", model.AlertStatusTriggered, model.AlertStatusAcknowledged, true, 404},
		{"missing resolve", model.AlertStatusAcknowledged, model.AlertStatusResolved, true, 404},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newManualEventFixture(t)
			fixture.event.Status = test.status
			if err := fixture.handler.alertEventStore.UpdateEvent(t.Context(), &fixture.event); err != nil {
				t.Fatal(err)
			}
			if test.missing {
				fixture.event.ID = "missing"
			}
			if response := fixture.request(t, test.target); response.Code != test.want {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
			if fixture.queue.request.SourceKey != "" {
				t.Fatal("invalid transition queued a notification")
			}
		})
	}
}

func TestManualAlertEnrichesFrozenNotificationContext(t *testing.T) {
	fixture := newManualEventFixture(t)
	rule, err := fixture.handler.alertRuleStore.CreateAlertRule(t.Context(), &model.AlertRule{Name: "High Memory", Metric: "memory", Operator: "gt", Threshold: 15, Severity: model.AlertSeverityWarning, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	node, err := fixture.handler.nodeStore.UpsertNode(t.Context(), "edge-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB.Exec("UPDATE alert_events SET rule_id=?,node_id=? WHERE id=?", rule.ID, node.ID, fixture.event.ID); err != nil {
		t.Fatal(err)
	}
	response := fixture.request(t, model.AlertStatusAcknowledged)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	data := fixture.queue.request.Data
	for key, want := range map[string]string{"node.name": "edge-node", "rule.name": "High Memory", "metric.name": "memory", "metric.formatted_value": "53.00", "event.severity": "warning", "event.status": "acknowledged"} {
		if data[key] != want {
			t.Fatalf("%s=%q, want %q", key, data[key], want)
		}
	}
	if !strings.Contains(fixture.queue.request.Text, "High Memory on node edge-node") {
		t.Fatal("notification context missing")
	}
}

func TestManualAlertQueueFailureLeavesEventUnchanged(t *testing.T) {
	for _, failure := range []error{notificationqueue.ErrQueueCapacity, notificationqueue.ErrQueueUnavailable, errors.New("persistence failed")} {
		t.Run(failure.Error(), func(t *testing.T) {
			fixture := newManualEventFixture(t)
			fixture.queue.failure = failure
			response := fixture.request(t, model.AlertStatusAcknowledged)
			if response.Code < 500 {
				t.Fatal("failed queue reported success")
			}
			stored, err := fixture.handler.alertEventStore.GetEventByID(t.Context(), fixture.event.ID)
			if err != nil || stored.Status != model.AlertStatusTriggered {
				t.Fatal("queue failure changed alert status")
			}
		})
	}
}

func TestManualAlertRechecksSessionAndStateInsideTransaction(t *testing.T) {
	for _, kind := range []string{"revoke", "concurrent resolve"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newManualEventFixture(t)
			want := http.StatusUnauthorized
			fixture.queue.before = func() {
				if kind == "revoke" {
					if err := store.NewAdminStore(fixture.database.DB).RevokeSession(t.Context(), fixture.principal.Session.ID, model.NowUTC()); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := fixture.database.DB.Exec("UPDATE alert_events SET status='resolved' WHERE id=?", fixture.event.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if kind != "revoke" {
				want = http.StatusConflict
			}
			if response := fixture.request(t, model.AlertStatusAcknowledged); response.Code != want {
				t.Fatalf("status=%d %s", response.Code, response.Body.String())
			}
			if fixture.queue.request.SourceKey != "" {
				t.Fatal("stale authority/state queued message")
			}
		})
	}
}

func TestManualAlertBoundaryAndEnrichmentErrors(t *testing.T) {
	for _, kind := range []string{"anonymous", "queue missing", "closed database", "rule read failure", "node read failure", "node store optional"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newManualEventFixture(t)
			want := http.StatusInternalServerError
			switch kind {
			case "anonymous":
				fixture.principal = model.AdminPrincipal{}
				want = http.StatusUnauthorized
			case "queue missing":
				fixture.handler.SetNotifications(nil)
				want = http.StatusServiceUnavailable
			case "closed database":
				if err := fixture.database.Close(); err != nil {
					t.Fatal(err)
				}
			case "rule read failure":
				if _, err := fixture.database.DB.Exec("ALTER TABLE alert_rules RENAME TO missing_rules"); err != nil {
					t.Fatal(err)
				}
			case "node read failure":
				if _, err := fixture.database.DB.Exec("ALTER TABLE nodes RENAME TO missing_nodes"); err != nil {
					t.Fatal(err)
				}
			case "node store optional":
				fixture.handler.nodeStore = nil
				want = http.StatusOK
			}
			if response := fixture.request(t, model.AlertStatusAcknowledged); response.Code != want {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
		})
	}
}
