package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/store"
)

func newClosedNodeStore(t *testing.T) *store.NodeStore {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "diagnostics.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	return store.NewNodeStore(sqlite.DB)
}

// TestInitializeDiagnosticsGuards 覆盖诊断控制面的依赖门禁、开关解析与启停默认值。
func TestInitializeDiagnosticsGuards(t *testing.T) {
	t.Run("missing dependencies", func(t *testing.T) {
		if err := (&serverServices{}).initializeDiagnostics(nil); err == nil {
			t.Fatal("nil stores initialized diagnostics")
		}
		f := newCommercialFixture(t)
		if err := (&serverServices{}).initializeDiagnostics(&serverStores{sqlite: f.stores.sqlite}); err == nil {
			t.Fatal("incomplete stores initialized diagnostics")
		}
	})
	t.Run("invalid toggle", func(t *testing.T) {
		t.Setenv("BEAT_DIAGNOSTICS_ENABLED", "not-a-bool")
		f := newCommercialFixture(t)
		if err := (&serverServices{}).initializeDiagnostics(f.stores); err == nil {
			t.Fatal("non-boolean diagnostics toggle was accepted")
		}
	})
	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("BEAT_DIAGNOSTICS_ENABLED", "")
		f := newCommercialFixture(t)
		services := &serverServices{}
		if err := services.initializeDiagnostics(f.stores); err != nil {
			t.Fatal(err)
		}
		if services.diagnostics == nil || services.diagnostics.Enabled() {
			t.Fatalf("default diagnostics state = %+v", services.diagnostics)
		}
	})
	t.Run("enabled by toggle", func(t *testing.T) {
		t.Setenv("BEAT_DIAGNOSTICS_ENABLED", "true")
		f := newCommercialFixture(t)
		services := &serverServices{}
		if err := services.initializeDiagnostics(f.stores); err != nil {
			t.Fatal(err)
		}
		if services.diagnostics == nil || !services.diagnostics.Enabled() {
			t.Fatal("diagnostics toggle did not enable the control plane")
		}
	})
}

// TestDiagnosticNodeLabels 覆盖节点显示名解析的不可用、未知与成功三条路径。
func TestDiagnosticNodeLabels(t *testing.T) {
	if _, err := (diagnosticNodeLabels{}).NodeName(t.Context(), "node"); err == nil {
		t.Fatal("missing node store resolved a display name")
	}
	broken := diagnosticNodeLabels{nodes: newClosedNodeStore(t)}
	if _, err := broken.NodeName(t.Context(), "node"); err == nil {
		t.Fatal("unreadable node store resolved a display name")
	}
	f := newCommercialFixture(t)
	node := commercialNode(t, f, "diagnostic-target")
	labels := diagnosticNodeLabels{nodes: f.stores.nodes}
	if name, err := labels.NodeName(t.Context(), node.ID); err != nil || name != "diagnostic-target" {
		t.Fatalf("node display name = %q, %v", name, err)
	}
	if _, err := labels.NodeName(t.Context(), "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("unknown node resolved a display name")
	}
}

// TestDiagnosticRouterAndResolver 覆盖路由开关与目标解析。
func TestDiagnosticRouterAndResolver(t *testing.T) {
	if option := diagnosticRouterOption(nil); option == nil {
		t.Fatal("nil services returned no router option")
	}
	if option := diagnosticRouterOption(&serverServices{}); option == nil {
		t.Fatal("disabled diagnostics returned no router option")
	}
	addresses, err := resolveDiagnosticHost(t.Context(), "localhost")
	if err != nil || len(addresses) == 0 {
		t.Fatalf("localhost resolution = %v, %v", addresses, err)
	}
}

// TestStartDiagnosticWorkerRunsWhenEnabled 覆盖诊断维护 worker 的注册与首轮对账。
func TestStartDiagnosticWorkerRunsWhenEnabled(t *testing.T) {
	startDiagnosticWorker(backgroundConfig{services: &serverServices{}}, &backgroundTasks{})
	t.Setenv("BEAT_DIAGNOSTICS_ENABLED", "true")
	f := newCommercialFixture(t)
	if err := f.services.initializeDiagnostics(f.stores); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime := newRuntimeState()
	tasks := &backgroundTasks{cancel: cancel, runtime: runtime}
	config := backgroundConfig{ctx: ctx, services: f.services, runtime: runtime}
	startDiagnosticWorker(config, tasks)
	defer tasks.stop()
	awaitCommercial(t, func() bool {
		workers := runtime.workers.snapshot()
		return len(workers) == 1 && !workers[0].LastSuccess.IsZero()
	})
	tasks.stop()
}
