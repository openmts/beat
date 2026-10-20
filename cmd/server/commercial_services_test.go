package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestOperationScannerConfigurationRejectsInvalidStartupInput(t *testing.T) {
	for _, test := range []struct {
		executable, args string
		valid            bool
	}{{"", "", true}, {"", "[]", false}, {"relative", "", false}, {"/bin/true", "not-json", false}, {"/bin/true", "[1]", false}, {"/bin/true", "[]", true}, {"/bin/true", "[\"--fixed\"]", true}} {
		t.Run(test.executable+test.args, func(t *testing.T) {
			t.Setenv("BEAT_OPERATIONS_SCANNER_EXECUTABLE", test.executable)
			t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", test.args)
			_, err := operationScanner()
			if (err == nil) != test.valid {
				t.Fatal(err)
			}
		})
	}
}

func TestCommercialInitializationAndCloseAreBoundedAndIdempotent(t *testing.T) {
	f := newCommercialFixture(t)
	t.Setenv("BEAT_OPERATIONS_ENABLED", "true")
	if err := f.services.initializeCommercial(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	if f.services.operations == nil || f.services.evaluation == nil || f.services.evaluationRules == nil || f.services.evaluationStates == nil || alertEvaluationOption(f.services) == nil {
		t.Fatal("missing runtime service")
	}
	info, err := os.Stat(filepath.Join(f.directory, "operations-spool"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal(info, err)
	}
	f.services.closeCommercial()
	f.services.closeCommercial()
	if err := f.services.operations.Recover(t.Context()); err == nil {
		t.Fatal("closed spool still allowed reconciliation")
	}
}

func TestCommercialInitializationFailureCleansPartialSpool(t *testing.T) {
	for _, scenario := range []string{"boolean", "scanner", "spool", "service", "recovery", "evaluation"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCommercialFixture(t)
			inputs := *f.stores
			switch scenario {
			case "boolean":
				t.Setenv("BEAT_OPERATIONS_ENABLED", "invalid")
			case "scanner":
				t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", "[]")
			case "spool":
				if err := os.WriteFile(filepath.Join(f.directory, "operations-spool"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "recovery":
				if _, err := f.stores.sqlite.DB.Exec("DROP TABLE operation_tasks"); err != nil {
					t.Fatal(err)
				}
			case "evaluation":
				inputs.mts = nil
			case "service":
				inputs.sqlite = &store.SQLiteStore{}
			}
			if err := f.services.initializeCommercial(t.Context(), &inputs, f.directory); err == nil {
				t.Fatal("invalid startup succeeded")
			}
			f.services.closeCommercial()
			f.services.closeCommercial()
			if f.services.operationSpool != nil {
				if err := f.services.operationSpool.Close(); err != nil {
					t.Fatal("spool close was not idempotent", err)
				}
			}
		})
	}
}

func TestCommercialCancelledInitializationReturnsWithoutWorkers(t *testing.T) {
	f := newCommercialFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.services.initializeCommercial(ctx, f.stores, f.directory); err == nil {
		t.Fatal("cancelled initialization succeeded")
	}
	f.services.closeCommercial()
}

func TestCommercialInitializationRejectsMissingDependenciesWithoutPanic(t *testing.T) {
	f := newCommercialFixture(t)
	for _, missing := range []string{"services", "stores", "sqlite", "database", "secrets", "metrics", "queue"} {
		inputs := *f.stores
		stores := &inputs
		services := &serverServices{delivery: f.services.delivery}
		switch missing {
		case "services":
			services = nil
		case "stores":
			stores = nil
		case "sqlite":
			inputs.sqlite = nil
		case "database":
			inputs.sqlite = &store.SQLiteStore{}
		case "secrets":
			inputs.secrets = nil
		case "metrics":
			inputs.mts = nil
		case "queue":
			services.delivery = nil
		}
		if err := services.initializeCommercial(t.Context(), stores, f.directory); err == nil || err.Error() != "commercial service dependencies are unavailable" {
			t.Fatal(missing, err)
		}
	}
}
