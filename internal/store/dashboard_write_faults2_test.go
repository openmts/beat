package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func dashboardFaultExec(t *testing.T, fixture dashboardFixture, statement string) {
	t.Helper()
	if _, err := fixture.database.DB.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotWritesFailClosedOnDatabaseFaults(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardSnapshotStore(fixture.database.DB)
	dashboard := dashboardSave(t, fixture, fixture.personalWrite())
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_snapshot BEFORE INSERT ON dashboard_snapshots
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	write := SnapshotWrite{Actor: fixture.actor, Value: model.DashboardSnapshot{
		Name: "Fault", DashboardID: dashboard.ID, Revision: dashboard.Revision, Definition: dashboard.Definition}}
	if _, err := store.Create(t.Context(), write); err == nil {
		t.Fatal("blocked snapshot insert reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_snapshot")
	snapshot, err := store.Create(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_audit BEFORE INSERT ON admin_audit_events
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Revoke(t.Context(), snapshot.ID, fixture.actor); err == nil {
		t.Fatal("unaudited revoke reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_audit")
	stillActive, err := store.Get(t.Context(), snapshot.ID)
	if err != nil || stillActive.RevokedAt != nil {
		t.Fatalf("failed revoke changed the snapshot: %+v, %v", stillActive, err)
	}
	other := fixture.addActor(t, "snapshot-other", model.AdminRoleAdmin)
	if err := store.Revoke(t.Context(), snapshot.ID, other); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("非创建者撤销 = %v", err)
	}
	if err := store.Revoke(t.Context(), "missing", fixture.actor); err == nil {
		t.Fatal("unknown snapshot was revoked")
	}
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_revoke BEFORE UPDATE ON dashboard_snapshots
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Revoke(t.Context(), snapshot.ID, fixture.actor); err == nil {
		t.Fatal("blocked revoke update reported success")
	}
}

func TestLibraryPanelWritesFailClosedOnDatabaseFaults(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardPanelLibraryStore(fixture.database.DB)
	panel := model.DefaultMetricDashboard("personal").Definition.Panels[0]
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_library BEFORE INSERT ON dashboard_panel_library
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if _, err := store.Create(t.Context(), LibraryPanelWrite{Actor: fixture.actor,
		Value: model.DashboardLibraryPanel{Name: "CPU", Panel: panel}}); err == nil {
		t.Fatal("blocked library insert reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_library")
	created, err := store.Create(t.Context(), LibraryPanelWrite{Actor: fixture.actor,
		Value: model.DashboardLibraryPanel{Name: "CPU", Panel: panel}})
	if err != nil {
		t.Fatal(err)
	}
	other := fixture.addActor(t, "library-other", model.AdminRoleAdmin)
	if _, err := store.Update(t.Context(), LibraryPanelWrite{Actor: other,
		Value: model.DashboardLibraryPanel{ID: created.ID, Name: "Stolen", Panel: panel}}); !errors.Is(err, ErrLibraryPanelNotFound) {
		t.Fatalf("非所有者更新面板 = %v", err)
	}
	if err := store.Delete(t.Context(), created.ID, other); !errors.Is(err, ErrLibraryPanelNotFound) {
		t.Fatalf("非所有者删除面板 = %v", err)
	}
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_library_audit BEFORE INSERT ON admin_audit_events
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if _, err := store.Update(t.Context(), LibraryPanelWrite{Actor: fixture.actor,
		Value: model.DashboardLibraryPanel{ID: created.ID, Name: "CPU 2", Panel: panel}}); err == nil {
		t.Fatal("unaudited library update reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_library_audit")
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_library_delete BEFORE DELETE ON dashboard_panel_library
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err == nil {
		t.Fatal("blocked library delete reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_library_delete")
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err != nil {
		t.Fatalf("删除自己的面板 = %v", err)
	}
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err == nil {
		t.Fatal("重复删除面板必须失败")
	}
}

func TestAnnotationWritesFailClosedOnDatabaseFaults(t *testing.T) {
	fixture := newDashboardFixture(t)
	store := NewDashboardAnnotationStore(fixture.database.DB)
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_annotation BEFORE INSERT ON dashboard_annotations
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if _, err := store.Create(t.Context(), collaborationAnnotation(fixture)); err == nil {
		t.Fatal("blocked annotation insert reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_annotation")
	created, err := store.Create(t.Context(), collaborationAnnotation(fixture))
	if err != nil {
		t.Fatal(err)
	}
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_annotation_audit BEFORE INSERT ON admin_audit_events
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	updated := *created
	updated.Title = "Deploy 1.3"
	if _, err := store.Update(t.Context(), AnnotationWrite{Actor: fixture.actor, Value: updated}); err == nil {
		t.Fatal("unaudited annotation update reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_annotation_audit")
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_annotation_delete BEFORE DELETE ON dashboard_annotations
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err == nil {
		t.Fatal("blocked annotation delete reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_annotation_delete")
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err != nil {
		t.Fatalf("删除自己的注释 = %v", err)
	}
	if err := store.Delete(t.Context(), created.ID, fixture.actor); err == nil {
		t.Fatal("重复删除注释必须失败")
	}
}

func TestPanelAlertReplaceFailsClosedOnDatabaseFaults(t *testing.T) {
	fixture := newDashboardFixture(t)
	dashboard := dashboardSave(t, fixture, fixture.personalWrite())
	panelID := dashboard.Definition.Panels[0].ID
	dashboardFaultExec(t, fixture, `INSERT INTO alert_rules(id,name,metric,operator,threshold,severity,enabled,created_at,updated_at)
		VALUES('cov-rule','Cov','cpu','gt',80,'warning',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	store := NewDashboardPanelAlertStore(fixture.database.DB)
	if err := store.Replace(t.Context(), dashboard.ID, panelID, []string{"cov-rule"}, fixture.actor); err != nil {
		t.Fatal(err)
	}
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_panel_alert_delete BEFORE DELETE ON dashboard_panel_alerts
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Replace(t.Context(), dashboard.ID, panelID, nil, fixture.actor); err == nil {
		t.Fatal("blocked panel alert clear reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_panel_alert_delete")
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_panel_alert_insert BEFORE INSERT ON dashboard_panel_alerts
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Replace(t.Context(), dashboard.ID, panelID, []string{"cov-rule"}, fixture.actor); err == nil {
		t.Fatal("blocked panel alert link reported success")
	}
	dashboardFaultExec(t, fixture, "DROP TRIGGER cov_block_panel_alert_insert")
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_panel_alert_audit BEFORE INSERT ON admin_audit_events
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if err := store.Replace(t.Context(), dashboard.ID, panelID, nil, fixture.actor); err == nil {
		t.Fatal("unaudited panel alert change reported success")
	}
}

func TestDashboardVersionPathsFailClosedOnDatabaseFaults(t *testing.T) {
	fixture := newDashboardFixture(t)
	first, second := fixture.twoRevisions(t)
	if _, err := fixture.database.DB.Exec("UPDATE dashboard_versions SET definition_json='{}' WHERE dashboard_id=? AND revision=?",
		first.ID, first.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.dashboards.VersionDefinition(t.Context(), first.ID, first.Revision); err == nil {
		t.Fatal("corrupt version definition was decoded")
	}
	if _, err := fixture.dashboards.VersionDefinition(t.Context(), first.ID, 9999); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatalf("未知版本 = %v", err)
	}
	if _, err := fixture.dashboards.RestoreVersion(t.Context(), DashboardVersionRestore{
		ID: second.ID, Revision: 9999, Actor: fixture.actor}); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatalf("恢复未知版本 = %v", err)
	}
	if _, err := fixture.dashboards.RestoreVersion(t.Context(), DashboardVersionRestore{
		ID: second.ID, Revision: second.Revision, Summary: string(make([]byte, 4096)), Actor: fixture.actor}); err == nil {
		t.Fatal("非法备注必须被拒绝")
	}
	dashboardFaultExec(t, fixture, `CREATE TRIGGER cov_block_version BEFORE INSERT ON dashboard_versions
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if _, err := fixture.dashboards.RestoreVersion(t.Context(), DashboardVersionRestore{
		ID: second.ID, Revision: first.Revision, Actor: fixture.actor}); err == nil {
		t.Fatal("blocked version restore reported success")
	}
}
