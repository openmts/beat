package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/remoteoperations"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

func (services *serverServices) initializeCommercial(ctx context.Context, stores *serverStores, dataDir string) error {
	if services == nil || stores == nil || stores.sqlite == nil || stores.sqlite.DB == nil || stores.secrets == nil || stores.mts == nil || stores.nodes == nil || services.delivery == nil || services.backups == nil {
		return fmt.Errorf("commercial service dependencies are unavailable")
	}
	scanner, err := operationScanner()
	if err != nil {
		return err
	}
	enabled := false
	if value := os.Getenv("BEAT_OPERATIONS_ENABLED"); value != "" {
		enabled, err = strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("BEAT_OPERATIONS_ENABLED must be a boolean")
		}
	}
	spool, err := remoteoperations.OpenSpool(remoteoperations.SpoolConfig{Directory: filepath.Join(dataDir, "operations-spool"), Scanner: scanner})
	if err != nil {
		return fmt.Errorf("initialize operation spool: %w", err)
	}
	services.operationSpool = spool
	operations, err := remoteoperations.NewService(remoteoperations.Config{Database: stores.sqlite.DB, Secrets: stores.secrets, Queue: services.delivery, Spool: spool, Enabled: enabled})
	if err != nil {
		return err
	}
	services.operations = operations
	if err := operations.Recover(ctx); err != nil {
		return fmt.Errorf("recover remote operations: %w", err)
	}
	services.evaluation, err = service.NewAlertEvaluator(service.AlertEvaluatorConfig{Database: stores.sqlite.DB, Metrics: stores.mts, Queue: services.delivery})
	if err != nil {
		return err
	}
	services.evaluationRules = store.NewAlertEvaluationRuleStore(stores.sqlite.DB)
	services.evaluationStates = store.NewAlertEvaluationStore(stores.sqlite.DB)
	if err := services.initializeNodeLifecycle(ctx, stores, dataDir); err != nil {
		return err
	}
	if err := services.initializeAccelerators(stores); err != nil {
		return err
	}
	if err := services.initializeAgentRollouts(ctx, stores, dataDir); err != nil {
		return err
	}
	if err := services.initializeServicePrincipals(ctx, stores); err != nil {
		return err
	}
	if err := services.initializeDiagnostics(stores); err != nil {
		return err
	}
	if err := services.initializeRuntimeDiagnostics(dataDir); err != nil {
		return fmt.Errorf("initialize runtime diagnostics service: %w", err)
	}
	return nil
}

func operationScanner() (remoteoperations.Scanner, error) {
	executable := os.Getenv("BEAT_OPERATIONS_SCANNER_EXECUTABLE")
	arguments := os.Getenv("BEAT_OPERATIONS_SCANNER_ARGS")
	if executable == "" {
		if arguments != "" {
			return nil, fmt.Errorf("operation scanner arguments require an executable")
		}
		return nil, nil
	}
	var args []string
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return nil, fmt.Errorf("operation scanner arguments must be a JSON string array")
		}
	}
	scanner, err := remoteoperations.NewCommandScanner(remoteoperations.CommandScannerConfig{Executable: executable, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("invalid operation scanner configuration")
	}
	return scanner, nil
}

func alertEvaluationOption(services *serverServices) api.RouterOption {
	return api.WithAlertEvaluation(handler.AlertEvaluationHandlerConfig{Rules: services.evaluationRules, States: services.evaluationStates, Preview: services.evaluation, Receipts: services.delivery})
}

func (services *serverServices) closeCommercial() {
	services.operationClose.Do(func() {
		if services.geoIP != nil {
			if err := services.geoIP.Close(); err != nil {
				slog.Error("close GeoIP failed", "reason", "geoip_close_failed")
			}
		}
		if services.operations != nil {
			services.operations.Close()
		}
		if services.operationSpool != nil {
			if err := services.operationSpool.Close(); err != nil {
				slog.Error("close operation spool failed", "reason", "spool_close_failed")
			}
		}
		services.closeRuntimeDiagnostics()
	})
}
