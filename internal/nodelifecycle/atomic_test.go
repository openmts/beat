package nodelifecycle

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func TestLifecycleOutboxDeliveryFailureRollsBackDateAndEvent(t *testing.T) {
	f := lifecycleFixture(t)
	_, err := f.queue.CreateChannel(t.Context(), notificationqueue.Actor{UserID: f.actor.UserID, SessionID: f.actor.SessionID}, model.NotificationChannelWrite{Name: "Lifecycle", ChannelType: "webhook", Enabled: true, EventKinds: []string{"node.renewed"}, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8/hook"}`)})
	if err != nil {
		t.Fatal(err)
	}
	input := validWrite()
	input.ExpiresOn = "2026-02-28"
	f.save(t, input)
	f.exec(t, `CREATE TRIGGER fail_lifecycle_delivery BEFORE INSERT ON notification_deliveries BEGIN SELECT RAISE(ABORT,'delivery');END`)
	input.ExpiresOn = "2026-03-31"
	if _, err = f.service.SaveNode(t.Context(), f.actor, NodeChange{NodeID: "life-node", LifecycleWrite: input}); err == nil {
		t.Fatal("delivery failure committed renewal")
	}
	value, err := f.service.Node(t.Context(), f.actor, "life-node")
	if err != nil || value.ExpiresOn != "2026-02-28" {
		t.Fatal(value, err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM node_lifecycle_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	f.exec(t, `DROP TRIGGER fail_lifecycle_delivery`)
	f.save(t, input)
	events, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 1 || events[0].RoutingStatus != "routed" {
		t.Fatal(events, err)
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestLifecycleConcurrentSchedulerAndExpiryEditStartNewPeriod(t *testing.T) {
	f := lifecycleFixture(t)
	input := validWrite()
	input.ExpiresOn = "2026-01-31"
	f.save(t, input)
	var wait sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wait.Add(1)
		go func() { defer wait.Done(); results <- f.service.Tick(t.Context()) }()
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	period := events[0].PeriodKey
	input.ExpiresOn = "2026-02-28"
	f.save(t, input)
	input.ExpiresOn = "2026-01-31"
	f.save(t, input)
	if err = f.service.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	events, err = f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 4 {
		t.Fatal(events, err)
	}
	reminders := 0
	for _, event := range events {
		if event.Kind == "node.expiring" {
			reminders++
			if event.PeriodKey == period {
				period = "seen-original"
			}
		}
	}
	if reminders != 2 || period != "seen-original" {
		t.Fatal("old period reminders reused", events)
	}
}

func TestAllReminderStagesAndAdvancementCap(t *testing.T) {
	f := lifecycleFixture(t)
	input := validWrite()
	input.ExpiresOn = "2026-03-31"
	f.save(t, input)
	expiry, _ := ParseDate(input.ExpiresOn)
	for _, remaining := range []int{30, 14, 7, 3, 1, 0, -1, -7} {
		f.now = expiry.AddDate(0, 0, -remaining)
		if err := f.service.Tick(t.Context()); err != nil {
			t.Fatal(remaining, err)
		}
	}
	events, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 8 {
		t.Fatal(events, err)
	}
	input.ExpiresOn = "2000-01-01"
	input.RenewalMode = "calendar_auto_advance"
	input.ConfirmAutoAdvance = true
	f.save(t, input)
	if err = f.service.Tick(t.Context()); !errors.Is(err, ErrAdvanceLimit) {
		t.Fatal(err)
	}
	value, err := f.service.Node(t.Context(), f.actor, "life-node")
	if err != nil || value.ExpiresOn != "2000-01-01" {
		t.Fatal("partial catchup", value, err)
	}
	f.now = time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
}

func TestEventsPageChronologicalCursorAndReadPermissions(t *testing.T) {
	f := lifecycleFixture(t)
	input := validWrite()
	input.ExpiresOn = "2026-02-01"
	f.save(t, input)
	input.ExpiresOn = "2026-03-01"
	f.save(t, input)
	f.now = f.now.Add(time.Second)
	input.ExpiresOn = "2026-04-01"
	f.save(t, input)
	first, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node", Limit: 1})
	if err != nil || len(first) != 1 || first[0].NewExpiresOn != "2026-04-01" {
		t.Fatal(first, err)
	}
	second, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node", Limit: 1, Before: first[0].ID})
	if err != nil || len(second) != 1 || second[0].NewExpiresOn != "2026-03-01" {
		t.Fatal(second, err)
	}
	if _, err = f.service.Events(t.Context(), Actor{}, EventQuery{NodeID: "life-node"}); err != ErrForbidden {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE admin_sessions SET revoked_at=? WHERE id=?`, f.now, f.actor.SessionID)
	if _, err = f.service.SaveNode(t.Context(), f.actor, NodeChange{NodeID: "life-node", LifecycleWrite: input}); err != ErrForbidden {
		t.Fatal(err)
	}
}
