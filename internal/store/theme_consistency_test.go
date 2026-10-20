package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestThemeLifecycleAndAuditCommitTogether(t *testing.T) {
	fixture := newDashboardFixture(t)
	themes := NewThemeStore(fixture.database.DB)
	ctx := WithThemeActor(t.Context(), ThemeActor(fixture.actor))
	pkg, now := sampleThemePackage("lifecycle"), model.NowUTC()
	if err := themes.CreatePackage(ctx, pkg); err != nil {
		t.Fatal(err)
	}
	if err := themes.UpdateConfiguration(ctx, ThemeConfigurationUpdate{PackageID: pkg.ID, JSON: `{"overview":"tiles"}`, At: now}); err != nil {
		t.Fatal(err)
	}
	activation := &model.ThemeActivation{ID: "active", PackageID: pkg.ID, ActorID: fixture.actor.UserID, Reason: model.ThemeActivationReason}
	if err := themes.ActivatePackage(ctx, activation, now); err != nil {
		t.Fatal(err)
	}
	if _, err := themes.DeletePackage(ctx, pkg.ID, now); !errors.Is(err, ErrThemePackageActive) {
		t.Fatalf("active delete: %v", err)
	}
	if err := themes.ActivatePackage(ctx, &model.ThemeActivation{ID: "builtin", Reason: model.ThemeFallbackReason}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := themes.DeletePackage(ctx, pkg.ID, now); err != nil {
		t.Fatal(err)
	}
	deleted, err := themes.ListDeletedPackages(ctx)
	if err != nil || len(deleted) != 1 {
		t.Fatalf("deleted listing: %+v %v", deleted, err)
	}
	if err := themes.CompletePackageDeletion(ctx, pkg.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := themes.RestorePackage(ctx, pkg); err != nil {
		t.Fatal(err)
	}
	got, err := themes.GetPackageByID(ctx, pkg.ID)
	if err != nil || got.ConfigurationJSON != `{"overview":"tiles"}` || got.State != model.ThemeStateInstalled || got.LastErrorCategory != "" {
		t.Fatalf("restored metadata: %+v %v", got, err)
	}
	current, err := themes.CurrentActivation(ctx)
	if err != nil || current.PackageID != "" {
		t.Fatalf("restore activated: %+v %v", current, err)
	}
	var count int
	err = fixture.database.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events WHERE actor_id=? AND resource_id=? AND action IN ('theme.install','theme.delete','theme.restore')`, fixture.actor.UserID, pkg.ID).Scan(&count)
	if err != nil || count != 3 {
		t.Fatalf("missing atomic resource audits: %d %v", count, err)
	}
}

func TestThemeMutationsDenyStaleAuthorityAndMissingRecentProof(t *testing.T) {
	fixture := newDashboardFixture(t)
	themes := NewThemeStore(fixture.database.DB)
	admin := fixture.addActor(t, "ordinary-admin", model.AdminRoleAdmin)
	for _, actor := range []ThemeActor{{}, ThemeActor(admin), {UserID: fixture.actor.UserID, SessionID: "missing"}} {
		if err := themes.CreatePackage(WithThemeActor(t.Context(), actor), sampleThemePackage("forbidden")); !errors.Is(err, ErrThemeForbidden) {
			t.Fatalf("authority bypass: %v", err)
		}
	}
	if _, err := fixture.database.DB.Exec(`UPDATE admin_sessions SET reauthenticated_until=NULL WHERE id=?`, fixture.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	ctx := WithThemeActor(t.Context(), ThemeActor(fixture.actor))
	if err := themes.CreatePackage(ctx, sampleThemePackage("expired")); !errors.Is(err, ErrThemeRecentAuth) {
		t.Fatalf("missing proof: %v", err)
	}
}

func TestThemeAuditFailureLeavesEveryPackageFieldUnchanged(t *testing.T) {
	themes, database := setupThemeStore(t)
	pkg := sampleThemePackage("retained")
	if err := themes.CreatePackage(t.Context(), pkg); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER fail_theme_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit offline'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := themes.DeletePackage(t.Context(), pkg.ID, model.NowUTC()); err == nil {
		t.Fatal("unaudited delete succeeded")
	}
	if err := themes.UpdateConfiguration(t.Context(), ThemeConfigurationUpdate{PackageID: pkg.ID, JSON: `{"density":"compact"}`, At: model.NowUTC()}); err == nil {
		t.Fatal("unaudited configuration succeeded")
	}
	if err := themes.ActivatePackage(t.Context(), &model.ThemeActivation{ID: "failed", PackageID: pkg.ID, Reason: model.ThemeActivationReason}, model.NowUTC()); err == nil {
		t.Fatal("unaudited activation succeeded")
	}
	got, err := themes.GetPackageByID(t.Context(), pkg.ID)
	if err != nil || got.State != model.ThemeStateInstalled || got.ConfigurationJSON != "{}" {
		t.Fatalf("partial mutation: %+v %v", got, err)
	}
	active, err := themes.CurrentActivation(t.Context())
	if err != nil || active != nil {
		t.Fatalf("partial activation: %+v %v", active, err)
	}
}

func TestThemeRollbackCandidatesSkipDeletedAndUnhealthyLatestActivation(t *testing.T) {
	themes, _ := setupThemeStore(t)
	now := model.NowUTC()
	for _, id := range []string{"first", "second"} {
		if err := themes.CreatePackage(t.Context(), sampleThemePackage(id)); err != nil {
			t.Fatal(err)
		}
		if err := themes.ActivatePackage(t.Context(), &model.ThemeActivation{ID: "act-" + id, PackageID: id, PreviousPackageID: "forged", Reason: model.ThemeActivationReason}, now); err != nil {
			t.Fatal(err)
		}
	}
	current, err := themes.CurrentActivation(t.Context())
	if err != nil || current.PreviousPackageID != "first" {
		t.Fatalf("caller forged activation history: %+v %v", current, err)
	}
	if err := themes.MarkActivationUnhealthy(t.Context(), "runtime_error", now); err != nil {
		t.Fatal(err)
	}
	ids, err := themes.HealthyRollbackCandidates(t.Context(), "")
	if err != nil || len(ids) != 1 || ids[0] != "first" {
		t.Fatalf("unhealthy rollback offered: %v %v", ids, err)
	}
	if _, err := themes.DeletePackage(t.Context(), "first", now); err != nil {
		t.Fatal(err)
	}
	ids, err = themes.HealthyRollbackCandidates(t.Context(), "")
	if err != nil || len(ids) != 0 {
		t.Fatalf("deleted rollback offered: %v %v", ids, err)
	}
	if err := themes.ActivatePackage(t.Context(), &model.ThemeActivation{ID: "bad", PackageID: "first", Reason: model.ThemeActivationReason}, now); !errors.Is(err, ErrThemePackageNotFound) {
		t.Fatal(err)
	}
}

func TestThemeLifecycleRejectsMissingRecordsAndUnreadableStore(t *testing.T) {
	themes, database := setupThemeStore(t)
	if _, err := themes.DeletePackage(t.Context(), "missing", model.NowUTC()); !errors.Is(err, ErrThemePackageNotFound) {
		t.Fatal(err)
	}
	if err := themes.RestorePackage(t.Context(), sampleThemePackage("missing")); !errors.Is(err, ErrThemePackageConflict) {
		t.Fatal(err)
	}
	if err := themes.CompletePackageDeletion(t.Context(), "missing", model.NowUTC()); !errors.Is(err, ErrThemePackageNotFound) {
		t.Fatal(err)
	}
	if err := themes.UpdateConfiguration(t.Context(), ThemeConfigurationUpdate{PackageID: "missing"}); !errors.Is(err, ErrThemePackageNotFound) {
		t.Fatal(err)
	}
	if err := themes.ActivatePackage(t.Context(), &model.ThemeActivation{ID: "bad", PackageID: "missing", Reason: model.ThemeActivationReason}, model.NowUTC()); !errors.Is(err, ErrThemePackageNotFound) {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := themes.ListDeletedPackages(t.Context()); err == nil {
		t.Fatal("closed database accepted")
	}
	if _, err := themes.HealthyRollbackCandidates(t.Context(), ""); err == nil {
		t.Fatal("closed rollback query accepted")
	}
}
