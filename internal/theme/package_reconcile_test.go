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

func TestStartupReconcilesOwnedStagingAndUncommittedPackages(t *testing.T) {
	service, database := emptyThemeService(t)
	validated, err := ValidateArchive(validPackageBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(service.root, "staging", "theme-interrupted")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, themeOwnershipMarker), []byte(validated.Digest), 0o600); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(service.root, "packages", validated.Digest)
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, themeOwnershipMarker), []byte(validated.Digest), 0o600); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(service.root, "staging", "theme-user-files")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknown, "keep"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), service.root, "0.1.0", time.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging not cleaned: %v", err)
	}
	if _, err := os.Stat(packageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan still installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(service.root, "quarantine", validated.Digest, themeOwnershipMarker)); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(unknown, "keep")); err != nil || string(content) != "keep" {
		t.Fatalf("unknown path was removed: %s %v", content, err)
	}
}

func TestRestoreAuditFailureLeavesLogicalDeletionAndCanBeRetried(t *testing.T) {
	service, database, pkg := definitionService(t)
	raw, _, err := service.Export(t.Context(), pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER reject_restore BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'no audit'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(t.Context(), raw, model.ThemeSourceUpload, "", "owner"); err == nil {
		t.Fatal("restore without audit succeeded")
	}
	stored, err := service.store.GetPackageByID(t.Context(), pkg.ID)
	if err != nil || stored.State != model.ThemeStateDeleted {
		t.Fatalf("restore partially committed: %+v %v", stored, err)
	}
	if _, err := os.Stat(service.PackageDir(pkg.ArchiveDigest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed restore retained assets: %v", err)
	}
	if _, err := database.DB.Exec(`DROP TRIGGER reject_restore`); err != nil {
		t.Fatal(err)
	}
	if restored, err := service.Install(t.Context(), raw, model.ThemeSourceUpload, "", "owner"); err != nil || restored.ID != pkg.ID {
		t.Fatalf("restore retry: %+v %v", restored, err)
	}
	if _, err := service.DeletePackage(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
}

func TestDeletedPackageCleanupNeverFollowsAnUnknownSymlink(t *testing.T) {
	service, _, pkg := definitionService(t)
	outside := t.TempDir()
	marker := filepath.Join(outside, "keep")
	if err := os.WriteFile(marker, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(service.root, "quarantine", pkg.ArchiveDigest)
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	result, err := service.DeletePackage(t.Context(), pkg.ID, "owner")
	if err != nil || !result.CleanupPending {
		t.Fatalf("unknown path was replaced: %+v %v", result, err)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "untouched" {
		t.Fatalf("outside data changed: %s %v", content, err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	result, err = service.DeletePackage(t.Context(), pkg.ID, "owner")
	if err != nil || result.CleanupPending {
		t.Fatalf("cleanup retry failed: %+v %v", result, err)
	}
}

func TestExportAndActivationRejectArchiveTampering(t *testing.T) {
	service, _, pkg := definitionService(t)
	archive := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "archive.zip")
	if err := os.WriteFile(archive, []byte("different bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Export(t.Context(), pkg.ID); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("unverified export: %v", err)
	}
	if _, err := service.Activate(t.Context(), pkg.ID, "owner"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("corrupt archive activated: %v", err)
	}
	active, err := service.ActiveDescriptor(t.Context())
	if err != nil || !active.BuiltIn {
		t.Fatalf("active state changed on failure: %+v %v", active, err)
	}
}
