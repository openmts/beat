package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestThemeActivationRollsBackHistoryWhenSitePointerWriteFails(t *testing.T) {
	themes, database := setupThemeStore(t)
	for _, id := range []string{"old", "new"} {
		if err := themes.CreatePackage(t.Context(), sampleThemePackage(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := themes.ActivatePackage(t.Context(), &model.ThemeActivation{ID: "old-activation", PackageID: "old", Reason: model.ThemeActivationReason}, model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER fail_site_theme BEFORE UPDATE OF active_theme_package_id ON site_settings BEGIN SELECT RAISE(ABORT,'site pointer unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := themes.ActivatePackage(t.Context(), &model.ThemeActivation{ID: "new-activation", PackageID: "new", Reason: model.ThemeActivationReason}, model.NowUTC()); err == nil {
		t.Fatal("partial activation succeeded")
	}
	activation, err := themes.CurrentActivation(t.Context())
	if err != nil || activation.PackageID != "old" || activation.ID != "old-activation" {
		t.Fatalf("history changed without site pointer: %+v %v", activation, err)
	}
	var active string
	if err := database.DB.QueryRow(`SELECT active_theme_package_id FROM site_settings`).Scan(&active); err != nil || active != "old" {
		t.Fatalf("site pointer changed: %s %v", active, err)
	}
}
