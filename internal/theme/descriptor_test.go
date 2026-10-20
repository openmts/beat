package theme

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func definitionService(t *testing.T) (*Service, *store.SQLiteStore, *model.ThemePackage) {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := service.Install(t.Context(), validPackageBytes(t), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	return service, database, pkg
}

func TestDescribeConfigureResetAndPersistence(t *testing.T) {
	service, database, pkg := definitionService(t)
	descriptor, err := service.DescribePackage(t.Context(), pkg.ID)
	if err != nil || descriptor.Declarative == nil || descriptor.PreviewURL == "" {
		t.Fatalf("preview: %+v %v", descriptor, err)
	}
	before, err := service.ActiveDescriptor(t.Context())
	if err != nil || !before.BuiltIn {
		t.Fatalf("preview activated a theme: %+v %v", before, err)
	}
	configured, err := service.Configure(t.Context(), pkg.ID, []byte(`{"overview":"table","detail":"compact","density":"compact","radius":0}`))
	if err != nil || configured.Declarative.Radius != 0 || configured.Declarative.Overview != "table" {
		t.Fatalf("configuration: %+v %v", configured, err)
	}
	restarted, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), service.root, "0.1.0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Activate(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	active, err := restarted.ActiveDescriptor(t.Context())
	if err != nil || active.Declarative == nil || active.Declarative.Detail != "compact" {
		t.Fatalf("persisted descriptor: %+v %v", active, err)
	}
	reset, err := restarted.Configure(t.Context(), pkg.ID, []byte(`{}`))
	if err != nil || reset.Declarative.Radius != descriptor.Declarative.Radius || reset.Declarative.Overview != descriptor.Declarative.Overview {
		t.Fatalf("reset: %+v %v", reset, err)
	}
}

func TestConfigureValidationUnavailableAndWriteFailure(t *testing.T) {
	service, database, pkg := definitionService(t)
	for _, raw := range []string{`[]`, `null`, `{"radius":9}`, `{"radius":1.2}`, `{"overview":"code"}`, `{"secret":"bad"}`, `{} {}`} {
		if _, err := service.Configure(t.Context(), pkg.ID, []byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := service.DescribePackage(t.Context(), "missing"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal(err)
	}
	if _, err := service.Configure(t.Context(), "missing", []byte(`{}`)); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER reject_theme_config BEFORE UPDATE ON theme_packages BEGIN SELECT RAISE(ABORT, 'write failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Configure(t.Context(), pkg.ID, []byte(`{"radius":0}`)); err == nil {
		t.Fatal("write failure ignored")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DescribePackage(t.Context(), pkg.ID); err == nil {
		t.Fatal("closed store ignored")
	}
}

func TestCorruptDefinitionAndConfigurationFallBack(t *testing.T) {
	for _, mutation := range []string{"asset", "manifest", "config"} {
		t.Run(mutation, func(t *testing.T) {
			service, database, pkg := definitionService(t)
			if _, err := service.Activate(t.Context(), pkg.ID, "owner"); err != nil {
				t.Fatal(err)
			}
			if mutation == "asset" {
				if err := os.WriteFile(filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content", definitionName), []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if mutation == "manifest" {
				if _, err := database.DB.Exec(`UPDATE theme_packages SET manifest_json='{}' WHERE id=?`, pkg.ID); err != nil {
					t.Fatal(err)
				}
			} else if _, err := database.DB.Exec(`UPDATE theme_packages SET configuration_json='{"radius":99}' WHERE id=?`, pkg.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := service.DescribePackage(t.Context(), pkg.ID); err == nil {
				t.Fatal("corrupted package described")
			}
			active, err := service.ActiveDescriptor(t.Context())
			if err != nil || !active.BuiltIn {
				t.Fatalf("unsafe active: %+v %v", active, err)
			}
		})
	}
}

func TestSandboxConfigurationIsNotADeclarativeSetting(t *testing.T) {
	service, pkg := assetFixture(t)
	descriptor, err := service.DescribePackage(t.Context(), pkg.ID)
	if err != nil || descriptor.SandboxEntry == "" || descriptor.Declarative != nil {
		t.Fatalf("sandbox preview: %+v %v", descriptor, err)
	}
	if _, err := service.Configure(t.Context(), pkg.ID, []byte(`{"overview":"table"}`)); !errors.Is(err, ErrRuntimeIncompatible) {
		t.Fatalf("configured a script theme: %v", err)
	}
	if err := service.Delete(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DescribePackage(t.Context(), pkg.ID); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("quarantined preview: %v", err)
	}
}

func TestDescriptorRevalidatesDefinitionBeyondContentHash(t *testing.T) {
	service, database, pkg := definitionService(t)
	raw := []byte(`{"schema_version":1}`)
	if err := os.WriteFile(filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content", definitionName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var manifest model.ThemeManifest
	if err := json.Unmarshal([]byte(pkg.ManifestJSON), &manifest); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Files {
		if manifest.Files[index].Path == definitionName {
			manifest.Files[index] = manifestFile(definitionName, raw)
		}
	}
	metadata, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`UPDATE theme_packages SET manifest_json=? WHERE id=?`, string(metadata), pkg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DescribePackage(t.Context(), pkg.ID); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("invalid definition accepted after matching its hash: %v", err)
	}
}

func TestSandboxPreviewRequiresTheInstalledEntry(t *testing.T) {
	service, pkg := assetFixture(t)
	descriptor, err := service.DescribePackage(t.Context(), pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content", descriptor.SandboxEntry)
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DescribePackage(t.Context(), pkg.ID); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("missing entry preview: %v", err)
	}
	pkg.ManifestJSON = sampleManifest(t, nil)
	if _, err := service.describeStoredPackage(t.Context(), pkg); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("mismatched stored runtime: %v", err)
	}
}
