package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func collaborationAnnotation(fixture dashboardFixture) AnnotationWrite {
	now := model.NowUTC()
	return AnnotationWrite{Actor: fixture.actor, Value: model.DashboardAnnotation{
		DashboardID: model.PublicDashboardID, PanelID: "", Title: "Deploy 1.2", Text: "rolled out",
		Tags: []string{"deploy"}, Color: "blue", Visibility: model.AnnotationVisibilityPublic,
		From: now.Add(-time.Hour), To: now,
	}}
}

func TestAnnotationLifecycleAndVisibility(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardAnnotationStore(fixture.database.DB)
	reader := fixture.addActor(t, "viewer", model.AdminRoleAdmin)
	write := collaborationAnnotation(fixture)
	created, err := store.Create(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.List(t.Context(), AnnotationFilter{DashboardID: model.PublicDashboardID,
		From: model.NowUTC().Add(-24 * time.Hour), To: model.NowUTC().Add(24 * time.Hour), UserID: reader.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Fatalf("public annotation visible to another admin, got %d", len(other))
	}
	write.Value.Visibility, write.Value.Title = model.AnnotationVisibilityPrivate, "hidden"
	write.Value.ID = uuid.NewString()
	if _, err := store.Create(t.Context(), write); err != nil {
		t.Fatal(err)
	}
	hidden, err := store.List(t.Context(), AnnotationFilter{DashboardID: model.PublicDashboardID,
		From: model.NowUTC().Add(-24 * time.Hour), To: model.NowUTC().Add(24 * time.Hour), UserID: reader.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 {
		t.Fatalf("private annotation leaked to another admin: %d", len(hidden))
	}
	created.Title = "Deploy 1.3"
	updated, err := store.Update(t.Context(), AnnotationWrite{Actor: fixture.actor, Value: *created})
	if err != nil || updated.Title != "Deploy 1.3" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if err := store.Delete(t.Context(), created.ID, reader); err == nil {
		t.Fatal("a non-owner admin must not delete another author's annotation")
	}
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err != nil {
		t.Fatalf("owner deletes the annotation: %v", err)
	}
	if _, err := store.Get(t.Context(), created.ID); err == nil {
		t.Fatal("annotation should be gone")
	}
}

func TestAnnotationValidationRejectsUnknownDashboard(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardAnnotationStore(fixture.database.DB)
	write := collaborationAnnotation(fixture)
	write.Value.DashboardID = uuid.NewString()
	if _, err := store.Create(t.Context(), write); err == nil {
		t.Fatal("annotation on an unknown dashboard must fail")
	}
	write = collaborationAnnotation(fixture)
	write.Value.From, write.Value.To = write.Value.To, write.Value.From
	if _, err := store.Create(t.Context(), write); err == nil {
		t.Fatal("reversed window must fail")
	}
}

func TestSnapshotShareAndRevoke(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardSnapshotStore(fixture.database.DB)
	dashboard := dashboardSave(t, fixture, fixture.personalWrite())
	snapshot, err := store.Create(t.Context(), SnapshotWrite{Actor: fixture.actor, Value: model.DashboardSnapshot{
		Name: "Freeze", DashboardID: dashboard.ID, Revision: dashboard.Revision, Definition: dashboard.Definition}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Token) != 64 {
		t.Fatalf("token length = %d, want 64", len(snapshot.Token))
	}
	public, err := store.GetByToken(t.Context(), snapshot.Token)
	if err != nil || public.Name != "Freeze" || public.Token != "" {
		t.Fatalf("public snapshot = %+v, %v", public, err)
	}
	if err := store.Revoke(t.Context(), snapshot.ID, fixture.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetByToken(t.Context(), snapshot.Token); err == nil {
		t.Fatal("revoked snapshot must not resolve")
	}
}

func TestSnapshotExpiryHidesLink(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardSnapshotStore(fixture.database.DB)
	expired := model.NowUTC().Add(-time.Minute)
	snapshot, err := store.Create(t.Context(), SnapshotWrite{Actor: fixture.actor, Value: model.DashboardSnapshot{
		Name: "Expired", DashboardID: model.PublicDashboardID, Revision: 1,
		Definition: model.DefaultMetricDashboard("public").Definition, ExpiresAt: &expired}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetByToken(t.Context(), snapshot.Token); err == nil {
		t.Fatal("expired snapshot must not resolve")
	}
}

func TestLibraryPanelLifecycleAndLimit(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardPanelLibraryStore(fixture.database.DB)
	panel := model.DefaultMetricDashboard("personal").Definition.Panels[0]
	write := LibraryPanelWrite{Actor: fixture.actor, Value: model.DashboardLibraryPanel{Name: "CPU", Panel: panel}}
	created, err := store.Create(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	created.Name = "CPU 2"
	updated, err := store.Update(t.Context(), LibraryPanelWrite{Actor: fixture.actor, Value: *created})
	if err != nil || updated.Name != "CPU 2" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if _, err := store.Create(t.Context(), LibraryPanelWrite{Actor: fixture.actor,
		Value: model.DashboardLibraryPanel{Name: "Bad", Panel: model.DashboardPanel{ID: "x"}}}); err == nil {
		t.Fatal("invalid panel must be rejected")
	}
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), created.ID); err == nil {
		t.Fatal("library panel should be deleted")
	}
}

func TestPanelAlertLinksAggregateState(t *testing.T) {
	fixture := newDashboardFixture(t)
	dashboard := dashboardSave(t, fixture, fixture.personalWrite())
	panelID := dashboard.Definition.Panels[0].ID
	if _, err := fixture.database.DB.Exec(`INSERT INTO alert_rules(id,name,metric,operator,threshold,severity,enabled,created_at,updated_at)
		VALUES('rule-1','CPU high','cpu','gt',80,'warning',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	store := NewDashboardPanelAlertStore(fixture.database.DB)
	if err := store.Replace(t.Context(), dashboard.ID, panelID, []string{"rule-1"}, fixture.actor); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(t.Context(), dashboard.ID, panelID, []string{"missing-rule"}, fixture.actor); err == nil {
		t.Fatal("unknown rule must be rejected")
	}
	insertEvaluationState(t, fixture, "rule-1", model.AlertPhaseFiring, model.EvaluationOK)
	insertEvaluationState(t, fixture, "rule-1", model.AlertPhaseNormal, model.EvaluationOK)
	values, err := store.List(t.Context(), dashboard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].RuleID != "rule-1" || values[0].RuleName != "CPU high" {
		t.Fatalf("panel links = %+v", values)
	}
	if values[0].State == nil || values[0].State.Phase != model.AlertPhaseFiring || values[0].State.Firing != 1 || values[0].State.Normal != 1 {
		t.Fatalf("aggregated state = %+v", values[0].State)
	}
	if err := store.Replace(t.Context(), dashboard.ID, panelID, nil, fixture.actor); err != nil {
		t.Fatal(err)
	}
	empty, err := store.List(t.Context(), dashboard.ID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("links after clear = %+v, %v", empty, err)
	}
}

func insertEvaluationState(t *testing.T, fixture dashboardFixture, ruleID, phase, status string) {
	t.Helper()
	if _, err := fixture.database.DB.Exec(`INSERT INTO alert_evaluation_states
		(rule_id,node_id,device_id,rule_revision,state_version,phase,evaluation_status,reason_code,next_due_at,updated_at)
		VALUES(?,?,?,1,1,?,?,'',?,?)`, ruleID, uuid.NewString(), "", phase, status,
		model.NowUTC().UnixMilli(), model.NowUTC().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotListGetAndLimit(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardSnapshotStore(fixture.database.DB)
	created, err := store.Create(t.Context(), SnapshotWrite{Actor: fixture.actor, Value: model.DashboardSnapshot{
		Name: "Freeze", DashboardID: model.PublicDashboardID, Revision: 1,
		Definition: model.DefaultMetricDashboard("public").Definition}})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(t.Context(), fixture.actor.UserID)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("snapshot list = %+v, %v", listed, err)
	}
	found, err := store.Get(t.Context(), created.ID)
	if err != nil || found.Name != "Freeze" {
		t.Fatalf("snapshot get = %+v, %v", found, err)
	}
	if _, err := store.Get(t.Context(), "missing"); err == nil {
		t.Fatal("missing snapshot must fail")
	}
	fillSnapshotLimit(t, fixture, model.MaxSnapshotsPerOwner)
	if _, err := store.Create(t.Context(), SnapshotWrite{Actor: fixture.actor, Value: model.DashboardSnapshot{
		Name: "Overflow", DashboardID: model.PublicDashboardID, Revision: 1,
		Definition: model.DefaultMetricDashboard("public").Definition}}); err == nil {
		t.Fatal("snapshot limit must be enforced")
	}
}

func fillSnapshotLimit(t *testing.T, fixture dashboardFixture, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		// token 必须 64 字符且唯一：这里用序号补足，避免随机后缀碰撞触发 UNIQUE 约束。
		token := strings.Repeat("f", 60) + fmt.Sprintf("%04x", index)
		if _, err := fixture.database.DB.Exec(`INSERT INTO dashboard_snapshots
			(id,token,name,dashboard_id,revision,definition_json,created_at,created_by)
			VALUES(?,?,?,?,1,'{}',?,?)`, uuid.NewString(),
			token, "filler", model.PublicDashboardID, model.NowUTC(),
			fixture.actor.UserID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLibraryPanelListAndLimit(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardPanelLibraryStore(fixture.database.DB)
	panel := model.DefaultMetricDashboard("personal").Definition.Panels[0]
	created, err := store.Create(t.Context(), LibraryPanelWrite{Actor: fixture.actor,
		Value: model.DashboardLibraryPanel{Name: "CPU", Panel: panel}})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(t.Context(), fixture.actor.UserID)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("library list = %+v, %v", listed, err)
	}
	if _, err := store.Update(t.Context(), LibraryPanelWrite{Actor: fixture.actor, Value: model.DashboardLibraryPanel{
		ID: "missing", Name: "CPU", Panel: panel}}); err == nil {
		t.Fatal("updating an unknown library panel must fail")
	}
	fillLibraryLimit(t, fixture, model.MaxLibraryPanelsPerOwner)
	if _, err := store.Create(t.Context(), LibraryPanelWrite{Actor: fixture.actor,
		Value: model.DashboardLibraryPanel{Name: "Overflow", Panel: panel}}); err == nil {
		t.Fatal("library limit must be enforced")
	}
}

func fillLibraryLimit(t *testing.T, fixture dashboardFixture, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if _, err := fixture.database.DB.Exec(`INSERT INTO dashboard_panel_library
			(id,owner_user_id,name,description,panel_json,created_at,updated_at) VALUES(?,?,?,'','{}',?,?)`,
			uuid.NewString(), fixture.actor.UserID, "filler", model.NowUTC(), model.NowUTC()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnnotationUpdateMissingAndLimit(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardAnnotationStore(fixture.database.DB)
	write := collaborationAnnotation(fixture)
	write.Value.ID = uuid.NewString()
	if _, err := store.Update(t.Context(), write); err == nil {
		t.Fatal("updating an unknown annotation must fail")
	}
	fillAnnotationLimit(t, fixture)
	if _, err := store.Create(t.Context(), collaborationAnnotation(fixture)); err == nil {
		t.Fatal("annotation limit must be enforced")
	}
}

func fillAnnotationLimit(t *testing.T, fixture dashboardFixture) {
	t.Helper()
	tags := "[]"
	for index := 0; index < model.MaxAnnotationsPerDashboard; index++ {
		from, to := model.NowUTC().Add(-time.Hour), model.NowUTC()
		if _, err := fixture.database.DB.Exec(`INSERT INTO dashboard_annotations
			(id,dashboard_id,panel_id,title,text,tags_json,color,visibility,from_ts,to_ts,created_at,updated_at,created_by,updated_by)
			VALUES(?,?, '', 'filler', '', ?, 'blue', 'public', ?, ?, ?, ?, ?, ?)`, uuid.NewString(),
			model.PublicDashboardID, tags, from, to, from, to, fixture.actor.UserID, fixture.actor.UserID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPanelAlertStateBuckets(t *testing.T) {
	fixture := newDashboardFixture(t)
	dashboard := dashboardSave(t, fixture, fixture.personalWrite())
	panelID := dashboard.Definition.Panels[0].ID
	if _, err := fixture.database.DB.Exec(`INSERT INTO alert_rules(id,name,metric,operator,threshold,severity,enabled,created_at,updated_at)
		VALUES('rule-states','CPU','cpu','gt',80,'warning',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	store := NewDashboardPanelAlertStore(fixture.database.DB)
	if err := store.Replace(t.Context(), dashboard.ID, panelID, []string{"rule-states"}, fixture.actor); err != nil {
		t.Fatal(err)
	}
	insertEvaluationState(t, fixture, "rule-states", model.AlertPhasePending, model.EvaluationOK)
	insertEvaluationState(t, fixture, "rule-states", model.AlertPhaseNormal, model.EvaluationNoData)
	insertEvaluationState(t, fixture, "rule-states", model.AlertPhaseNormal, model.EvaluationError)
	values, err := store.List(t.Context(), dashboard.ID)
	if err != nil || len(values) != 1 {
		t.Fatalf("panel links = %+v, %v", values, err)
	}
	state := values[0].State
	if state == nil || state.Pending != 1 || state.NoData != 1 || state.Error != 1 || state.Phase != model.AlertPhasePending {
		t.Fatalf("state buckets = %+v", state)
	}
	if err := store.Replace(t.Context(), dashboard.ID, panelID, []string{"rule-states", "rule-states"}, fixture.actor); err == nil {
		t.Fatal("duplicate rule ids must be rejected")
	}
	if err := store.Replace(t.Context(), dashboard.ID, "missing-panel", []string{"rule-states", "rule-states", "a", "b", "c"}, fixture.actor); err == nil {
		t.Fatal("more than four rules must be rejected")
	}
}

func TestDashboardStoreExposesSharedConnection(t *testing.T) {
	fixture := newDashboardFixture(t)
	if NewDashboardStore(fixture.database.DB).DB() != fixture.database.DB {
		t.Fatal("dashboard store returned a different connection")
	}
}
