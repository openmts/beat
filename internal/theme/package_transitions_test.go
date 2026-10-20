package theme

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestRollbackSkipsDeletedMissingAndUnhealthyPackages(t *testing.T) {
	for _, failure := range []string{"deleted", "missing", "unhealthy"} {
		t.Run(failure, func(t *testing.T) {
			service, database, first := definitionService(t)
			raw := buildThemeZip(t, sampleManifest(t, map[string]any{"version": "2.0.0"}), map[string]string{"preview.png": fixturePNG, "assets/theme.css": "c"})
			second, err := service.Install(t.Context(), raw, model.ThemeSourceUpload, "", "owner")
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{first.ID, second.ID} {
				if _, err := service.Activate(t.Context(), id, "owner"); err != nil {
					t.Fatal(err)
				}
			}
			switch failure {
			case "deleted":
				if err := service.Delete(t.Context(), first.ID, "owner"); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(service.PackageDir(first.ArchiveDigest), "content", definitionName)); err != nil {
					t.Fatal(err)
				}
			case "unhealthy":
				if _, err := database.DB.Exec(`UPDATE theme_activations SET health_state='unhealthy' WHERE package_id=?`, first.ID); err != nil {
					t.Fatal(err)
				}
			}
			target, err := service.Rollback(t.Context(), "owner")
			if err != nil || target != "built-in" {
				t.Fatalf("unsafe rollback target: %s %v", target, err)
			}
		})
	}
}

func TestConcurrentActivationAndDeletionHaveOneConsistentWinner(t *testing.T) {
	for range 6 {
		service, _, pkg := definitionService(t)
		start, activated, deleted := make(chan struct{}), make(chan error, 1), make(chan error, 1)
		go func() { <-start; _, err := service.Activate(t.Context(), pkg.ID, "owner"); activated <- err }()
		go func() { <-start; deleted <- service.Delete(t.Context(), pkg.ID, "owner") }()
		close(start)
		activationErr, deleteErr := <-activated, <-deleted
		if (activationErr == nil) == (deleteErr == nil) {
			t.Fatalf("inconsistent winners: activation=%v deletion=%v", activationErr, deleteErr)
		}
		stored, err := service.store.GetPackageByID(t.Context(), pkg.ID)
		if err != nil {
			t.Fatal(err)
		}
		active, err := service.ActiveDescriptor(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if activationErr == nil && (active.PackageID != pkg.ID || stored.State != model.ThemeStateInstalled) {
			t.Fatal("activated a deleted package")
		}
		if deleteErr == nil && (!active.BuiltIn || stored.State != model.ThemeStateDeleted) {
			t.Fatal("deleted package remained active")
		}
	}
}

func TestCanceledThemeMutationsNeverEnterOrLeakTheOperationGate(t *testing.T) {
	service, _, pkg := definitionService(t)
	raw, _, err := service.Export(t.Context(), pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for name, operation := range map[string]func() error{
		"install":   func() error { _, err := service.Install(ctx, raw, model.ThemeSourceUpload, "", "owner"); return err },
		"configure": func() error { _, err := service.Configure(ctx, pkg.ID, []byte(`{}`)); return err },
		"activate":  func() error { _, err := service.Activate(ctx, pkg.ID, "owner"); return err },
		"rollback":  func() error { _, err := service.Rollback(ctx, "owner"); return err },
		"fallback":  func() error { return service.Fallback(ctx, "owner", "test") },
		"unhealthy": func() error { return service.MarkUnhealthy(ctx, "test") },
		"delete":    func() error { return service.Delete(ctx, pkg.ID, "owner") },
	} {
		if err := operation(); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s ignored cancellation: %v", name, err)
		}
	}
	release, err := service.mutations.enter(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	queued, cancelQueued := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- service.Delete(queued, pkg.ID, "owner") }()
	cancelQueued()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	if _, err := service.Activate(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatalf("gate leaked after cancellation: %v", err)
	}
}
