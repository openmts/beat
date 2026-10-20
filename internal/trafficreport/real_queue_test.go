package trafficreport

import (
	"testing"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/testutil"
)

func TestScheduledReportRealOutboxRollsBackCursorAndRecovers(t *testing.T) {
	fixture := newReportFixture(t)
	secrets := testutil.NewSecrets(t, fixture.database.DB)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: fixture.database.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	fixture.service.queue = queue
	if _, err := fixture.database.DB.Exec(`CREATE TRIGGER reject_report_outbox BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'message failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.RunDue(t.Context()); err == nil {
		t.Fatal("outbox failure accepted")
	}
	schedule, err := fixture.store.Get(t.Context(), fixture.schedule.ID)
	if err != nil || schedule.LastPeriodKey != "" || !schedule.NextRunAt.Equal(fixture.schedule.NextRunAt) {
		t.Fatal("outbox failure consumed schedule period")
	}
	if _, err := fixture.database.DB.Exec("DROP TRIGGER reject_report_outbox"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	schedules, err := fixture.service.List(t.Context())
	if err != nil || schedules[0].LastDelivery.State != "not_routed" {
		t.Fatal("empty channel routing was hidden")
	}
	if err := fixture.service.runScheduled(t.Context(), fixture.schedule, fixture.now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.database.DB.QueryRow("SELECT COUNT(*) FROM notification_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("retry duplicated report message")
	}
}
