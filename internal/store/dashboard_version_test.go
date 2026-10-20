package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestDashboardVersionsRecordHistoryAndPrune(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	first := dashboardSave(t, fixture, write)
	write.ExpectedRevision, write.Summary = 1, "second layout"
	write.Value.Definition.Title = "second layout"
	second := dashboardSave(t, fixture, write)
	versions, err := fixture.dashboards.ListVersions(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Revision != 2 || versions[1].Revision != 1 {
		t.Fatalf("versions = %+v", versions)
	}
	if versions[0].Summary != "second layout" || versions[0].Title != "second layout" {
		t.Fatalf("version metadata = %+v", versions[0])
	}
	// 作者解析成用户名，不把内部用户 UUID 暴露给版本列表。
	if versions[0].CreatedBy != "owner" || versions[1].CreatedBy != "owner" {
		t.Fatalf("actors = %q / %q", versions[0].CreatedBy, versions[1].CreatedBy)
	}
	if versions[0].CreatedAt.IsZero() {
		t.Fatal("version timestamp missing")
	}
	// 逐条保存超过上限后只保留最近 MaxDashboardVersions 条，最旧的版本被裁掉。
	total := model.MaxDashboardVersions + 3
	for revision := second.Revision; revision < total; revision += 1 {
		write.ExpectedRevision = revision
		write.Value.Definition.Title = "layout " + strings.Repeat("x", revision%3)
		dashboardSave(t, fixture, write)
	}
	pruned, err := fixture.dashboards.ListVersions(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != model.MaxDashboardVersions {
		t.Fatalf("retained = %d", len(pruned))
	}
	if pruned[0].Revision != total || pruned[len(pruned)-1].Revision != total-model.MaxDashboardVersions+1 {
		t.Fatalf("retained range = %d..%d", pruned[0].Revision, pruned[len(pruned)-1].Revision)
	}
	// 没有历史的看板返回空列表而不是错误；未知看板同样返回空集合。
	empty, err := fixture.dashboards.ListVersions(t.Context(), "11111111-1111-4111-8111-111111111111")
	if err != nil || len(empty) != 0 {
		t.Fatalf("unknown dashboard = %+v / %v", empty, err)
	}
}

func TestDashboardVersionDefinitionReadsAndValidates(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	write.Value.Definition.Title = "original"
	first := dashboardSave(t, fixture, write)
	definition, err := fixture.dashboards.VersionDefinition(t.Context(), first.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Title != "original" || len(definition.Panels) == 0 {
		t.Fatalf("definition = %+v", definition)
	}
	if _, err := fixture.dashboards.VersionDefinition(t.Context(), first.ID, 4); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatalf("missing version = %v", err)
	}
	if _, err := fixture.dashboards.VersionDefinition(t.Context(), "22222222-2222-4222-8222-222222222222", 1); !errors.Is(err, ErrDashboardNotFound) {
		t.Fatalf("missing dashboard = %v", err)
	}
	// 历史定义损坏时读取必须报错，而不是把坏定义当成可恢复版本。
	// 表结构本身有 json_valid 约束，这里覆盖「JSON 合法但看板定义不合法」的历史读取。
	for name, encoded := range map[string]string{"invalid definition": `{"version":1,"title":"","panels":[]}`} {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.database.DB.Exec(
				"UPDATE dashboard_versions SET definition_json=? WHERE dashboard_id=? AND revision=1", encoded, first.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.dashboards.VersionDefinition(t.Context(), first.ID, 1); err == nil {
				t.Fatal("corrupted version accepted")
			}
			if _, err := fixture.database.DB.Exec(
				"UPDATE dashboard_versions SET definition_json=? WHERE dashboard_id=? AND revision=1", mustEncodeVersionDefinition(t, first.Definition), first.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDashboardVersionRestoreKeepsHistoryAndChecksConcurrency(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	write.Value.Definition.Title = "restore target"
	first := dashboardSave(t, fixture, write)
	if first.Revision != 1 || first.Definition.Title != "restore target" {
		t.Fatalf("first revision = %+v", first)
	}
	write.ExpectedRevision, write.Value.Definition.Title = 1, "current layout"
	current := dashboardSave(t, fixture, write)
	restored, err := fixture.dashboards.RestoreVersion(t.Context(), DashboardVersionRestore{
		ID: current.ID, Revision: 1, ExpectedRevision: current.Revision, Summary: "roll back", Actor: fixture.actor})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Revision != current.Revision+1 || restored.Definition.Title != "restore target" {
		t.Fatalf("restored = %+v", restored)
	}
	versions, err := fixture.dashboards.ListVersions(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || versions[0].Revision != restored.Revision || versions[0].Summary != "roll back" {
		t.Fatalf("versions = %+v", versions)
	}
	if versions[1].Title != "current layout" {
		t.Fatalf("history overwritten: %+v", versions[1])
	}
}

func TestDashboardVersionRestoreRejectsInvalidRequests(t *testing.T) {
	fixture := newDashboardFixture(t)
	write := fixture.personalWrite()
	saved := dashboardSave(t, fixture, write)
	target := saved.ID
	cases := []struct {
		name    string
		request DashboardVersionRestore
		expect  error
	}{
		{"stale expected revision", DashboardVersionRestore{ID: target, Revision: 1, ExpectedRevision: 5, Actor: fixture.actor}, ErrDashboardConflict},
		{"unknown dashboard", DashboardVersionRestore{ID: "33333333-3333-4333-8333-333333333333", Revision: 1, Actor: fixture.actor}, ErrDashboardNotFound},
		{"unknown revision", DashboardVersionRestore{ID: target, Revision: 9, ExpectedRevision: 1, Actor: fixture.actor}, ErrDashboardNotFound},
		{"foreign actor", DashboardVersionRestore{ID: target, Revision: 1, ExpectedRevision: 1,
			Actor: DashboardActor{UserID: "55555555-5555-4555-8555-555555555555", SessionID: "missing-session"}}, ErrDashboardForbidden},
		{"invalid summary", DashboardVersionRestore{ID: target, Revision: 1, ExpectedRevision: 1,
			Summary: "bad\vsummary", Actor: fixture.actor}, model.ErrInvalidDashboard},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.dashboards.RestoreVersion(t.Context(), testCase.request); !errors.Is(err, testCase.expect) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// 被拒绝的恢复不留下版本记录。
	versions, err := fixture.dashboards.ListVersions(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions after rejections = %+v", versions)
	}
}

func mustEncodeVersionDefinition(t *testing.T, definition model.DashboardDefinition) string {
	t.Helper()
	encoded, err := encodeDashboard(definition)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
