package theme

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExactArchiveDigestAndRootFailuresRemainUnavailable(t *testing.T) {
	service, _, pkg := definitionService(t)
	archive := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "archive.zip")
	if err := os.WriteFile(archive, bytes.Repeat([]byte{'x'}, int(pkg.ArchiveBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Export(t.Context(), pkg.ID); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("same-size archive tampering accepted: %v", err)
	}
	service.root = filepath.Join(t.TempDir(), "missing")
	if _, err := service.readPackageArchive(pkg); err == nil {
		t.Fatal("missing root accepted")
	}
	if err := service.quarantinePackage(pkg.ArchiveDigest); err == nil {
		t.Fatal("quarantine ignored missing root")
	}
	if err := service.clearDeletedContent(pkg.ArchiveDigest); err == nil {
		t.Fatal("restore ignored missing root")
	}
	if err := service.quarantinePackage("../outside"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal("invalid digest accepted")
	}
}

func TestStagingRejectsExistingFilesAndBlockedContentDirectories(t *testing.T) {
	service, _ := emptyThemeService(t)
	raw := validPackageBytes(t)
	validated, err := ValidateArchive(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []string{themeOwnershipMarker, "archive.zip", "content", "content/assets"} {
		t.Run(blocked, func(t *testing.T) {
			staging := t.TempDir()
			file := filepath.Join(staging, blocked)
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("obstruction"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := service.stageArchive(staging, validated, raw); err == nil {
				t.Fatal("blocked staging succeeded")
			}
		})
	}
}

func TestUnknownOwnershipMarkersArePreservedDuringCleanup(t *testing.T) {
	for _, marker := range []string{"", "too short", string(bytes.Repeat([]byte{'z'}, 64)), string(bytes.Repeat([]byte{'a'}, 64))} {
		rootName := t.TempDir()
		directory := filepath.Join(rootName, "quarantine")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, themeOwnershipMarker), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(rootName)
		if err != nil {
			t.Fatal(err)
		}
		err = removeOwnedThemeDirectory(root, "quarantine", string(bytes.Repeat([]byte{'b'}, 64)))
		_ = root.Close()
		if err == nil {
			t.Fatal("unrecognized marker was deleted")
		}
		if _, err := os.Stat(directory); err != nil {
			t.Fatal("unknown directory was removed")
		}
	}
}

func TestReconcileAndSyncReportUnavailableFilesystem(t *testing.T) {
	service, database := emptyThemeService(t)
	if err := syncStagedTheme(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing staging ignored")
	}
	if err := syncThemeDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory ignored")
	}
	root, err := os.OpenRoot(service.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readThemeDirectory(root, "staging"); err == nil {
		t.Fatal("closed root ignored")
	}
	if err := syncRootDirectory(root, "packages"); err == nil {
		t.Fatal("closed root sync ignored")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.reconcilePackages(context.Background()); err == nil {
		t.Fatal("closed store ignored")
	}
}
