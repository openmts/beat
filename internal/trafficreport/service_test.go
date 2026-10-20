package trafficreport

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestServiceScheduleLifecycle(t *testing.T) {
	fixture := newReportFixture(t)
	service := fixture.service
	created, err := service.Create(t.Context(), &model.TrafficReportSchedule{Name: " Daily ", Cadence: model.TrafficReportDaily, Timezone: "UTC", SendHour: 9, AllNodes: true, AllChannels: true, Enabled: true})
	if err != nil || created.Name != "Daily" || created.Weekday != 1 || created.MonthDay != 1 || !created.NextRunAt.After(fixture.now) {
		t.Fatalf("created=%+v/%v", created, err)
	}
	if schedules, err := service.List(t.Context()); err != nil || len(schedules) != 2 {
		t.Fatalf("list=%d/%v", len(schedules), err)
	}
	created.Name = "Changed"
	updated, err := service.Update(t.Context(), created.ID, created)
	if err != nil || updated.Name != "Changed" {
		t.Fatalf("updated=%+v/%v", updated, err)
	}
	if err := service.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), created.ID); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), created.ID, created); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatal(err)
	}
}

func TestServiceScheduleValidationAndDatabaseFailures(t *testing.T) {
	fixture := newReportFixture(t)
	for _, invalid := range []*model.TrafficReportSchedule{nil, {}, {Name: "bad", Cadence: model.TrafficReportDaily, Timezone: "Missing/Zone", AllNodes: true, AllChannels: true}} {
		if _, err := fixture.service.Create(t.Context(), invalid); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatal(err)
		}
		if _, err := fixture.service.Update(t.Context(), fixture.schedule.ID, invalid); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatal(err)
		}
	}
	if err := fixture.database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.List(t.Context()); err == nil {
		t.Fatal("closed list succeeded")
	}
	if err := fixture.service.RunDue(t.Context()); err == nil {
		t.Fatal("closed scheduler succeeded")
	}
	if _, err := fixture.service.Create(t.Context(), &fixture.schedule); err == nil {
		t.Fatal("closed create succeeded")
	}
	if _, err := fixture.service.Update(t.Context(), fixture.schedule.ID, &fixture.schedule); err == nil {
		t.Fatal("closed update succeeded")
	}
	if err := fixture.service.Delete(t.Context(), fixture.schedule.ID); err == nil {
		t.Fatal("closed delete succeeded")
	}
}

func TestScheduleNormalization(t *testing.T) {
	schedule := model.TrafficReportSchedule{Name: " Report ", Timezone: " UTC ", NodeIDs: []string{" one ", "one", "", "two"}, ChannelIDs: []string{"c", "c", " "}}
	normalizeSchedule(&schedule)
	if schedule.Name != "Report" || len(schedule.NodeIDs) != 2 || len(schedule.ChannelIDs) != 1 {
		t.Fatalf("normalization=%+v", schedule)
	}
	schedule.AllNodes, schedule.AllChannels = true, true
	normalizeSchedule(&schedule)
	if len(schedule.NodeIDs) != 0 || len(schedule.ChannelIDs) != 0 {
		t.Fatal("all scopes retained selections")
	}
	if got, err := NextRun(schedule, time.Now()); err == nil || !got.IsZero() {
		t.Fatal("missing cadence accepted")
	}
}
