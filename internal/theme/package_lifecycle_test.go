package theme

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestDeleteAndVerifiedReinstallPreserveIdentityConfigurationAndAudit(t *testing.T) {
	service, database, pkg := definitionService(t)
	raw, _, err := service.Export(t.Context(), pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Configure(t.Context(), pkg.ID, []byte(`{"overview":"tiles"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeletePackage(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	stored, err := service.store.GetPackageByID(t.Context(), pkg.ID)
	if err != nil || stored.State != model.ThemeStateDeleted {
		t.Fatalf("logical deletion: %+v %v", stored, err)
	}
	listed, err := service.ListPackages(t.Context())
	if err != nil || len(listed) != 0 {
		t.Fatalf("deleted package is listed: %+v %v", listed, err)
	}
	if _, err := service.ReadAsset(t.Context(), pkg.ArchiveDigest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal(err)
	}
	if _, _, err := service.Export(t.Context(), pkg.ID); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal(err)
	}
	restored, err := service.Install(t.Context(), raw, model.ThemeSourceUpload, "", "owner")
	if err != nil || restored.ID != pkg.ID || restored.ConfigurationJSON != `{"overview":"tiles"}` {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	descriptor, err := service.DescribePackage(t.Context(), restored.ID)
	if err != nil || descriptor.Declarative.Overview != "tiles" {
		t.Fatalf("restored configuration: %+v %v", descriptor, err)
	}
	active, err := service.ActiveDescriptor(t.Context())
	if err != nil || !active.BuiltIn {
		t.Fatalf("restore activated the package: %+v %v", active, err)
	}
	var count int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events WHERE resource_id=? AND action IN ('theme.install','theme.delete','theme.restore')`, pkg.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("audit history: %d %v", count, err)
	}
}

func TestThemeAuditFailureCannotDeleteOrHideTheInstalledPackage(t *testing.T) {
	service, database, pkg := definitionService(t)
	if _, err := database.DB.Exec(`CREATE TRIGGER block_theme_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeletePackage(t.Context(), pkg.ID, "owner"); err == nil {
		t.Fatal("delete succeeded without audit")
	}
	if _, err := service.DescribePackage(t.Context(), pkg.ID); err != nil {
		t.Fatalf("failed delete hid the package: %v", err)
	}
}

func TestLogicalDeletionSurvivesCleanupFailureAndStartupReconciles(t *testing.T) {
	service, database, pkg := definitionService(t)
	blocked := filepath.Join(service.root, "quarantine", pkg.ArchiveDigest)
	if err := os.WriteFile(blocked, []byte("owned obstruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := service.DeletePackage(t.Context(), pkg.ID, "owner")
	if err != nil || !result.CleanupPending || result.State != model.ThemeStateDeleted {
		t.Fatalf("partial cleanup response: %+v %v", result, err)
	}
	if _, err := service.ReadAsset(t.Context(), pkg.ArchiveDigest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("deleted assets remained served: %v", err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), service.root, "0.1.0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.store.GetPackageByID(t.Context(), pkg.ID)
	if err != nil || stored.LastErrorCategory != "deleted" {
		t.Fatalf("cleanup not reconciled: %+v %v", stored, err)
	}
	if _, err := os.Stat(filepath.Join(blocked, "archive.zip")); err != nil {
		t.Fatalf("quarantine not recovered: %v", err)
	}
	if _, err := restarted.DeletePackage(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatalf("idempotent cleanup retry failed: %v", err)
	}
}
