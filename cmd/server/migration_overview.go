package main

import (
	"context"
	"time"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/buildinfo"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const (
	migrationOverviewTimeout       = 3 * time.Second
	migrationOverviewStagedScanCap = 50
	// runbookHash 指向运维手册里的固定小节锚点，不暴露任何本机路径。
	migrationRunbookHash = "docs/beat/operations-runbook.md#sqlite-and-migrations"
)

// migrationOverviewSource 在运行中的服务端聚合只读迁移状态；它不修改任何数据。
type migrationOverviewSource struct {
	sqlite  *store.SQLiteStore
	mts     mtsMonitor
	backups backupMonitor
	dataDir string
	version string
}

func newMigrationOverviewSource(stores *serverStores, dataDir string) handler.MigrationOverviewSource {
	if stores == nil || stores.sqlite == nil {
		return nil
	}
	version := buildinfo.Version
	return migrationOverviewSource{sqlite: stores.sqlite, mts: stores.mts, backups: stores.backups, dataDir: dataDir, version: version}
}

func (source migrationOverviewSource) MigrationOverview(ctx context.Context) (handler.MigrationOverviewView, error) {
	ctx, cancel := context.WithTimeout(ctx, migrationOverviewTimeout)
	defer cancel()
	view := handler.MigrationOverviewView{Version: source.version, State: handler.MigrationStateCurrent, Code: handler.MigrationCodeOK,
		RunbookHash: migrationRunbookHash, ApplicationHealth: "ok", MetricsHealth: "ok",
		MinimumSupportedSchema: store.MinimumSupportedSchemaVersion, MaximumSupportedSchema: store.MaximumSupportedSchemaVersion,
		PendingMigrations: []handler.MigrationStepView{}}
	source.applyFailureMarker(&view)
	if err := source.applyRestoreState(ctx, &view); err != nil {
		return view, nil
	}
	source.applySchemaState(ctx, &view)
	source.applyMetricsHealth(&view)
	return view, nil
}

// applyFailureMarker 只暴露固定字段；标记缺失或不可读时保持默认状态。
func (source migrationOverviewSource) applyFailureMarker(view *handler.MigrationOverviewView) {
	marker, err := migrationstatus.Read(source.dataDir)
	if err != nil || marker.State != migrationstatus.StateFailed {
		return
	}
	view.State = handler.MigrationStateInconsistent
	view.Code = marker.FailureCode
	if view.Code == "" {
		view.Code = handler.MigrationCodeUnreadable
	}
	view.LastFailure = &handler.MigrationFailureView{UpdatedAt: marker.UpdatedAt, State: marker.State, Phase: marker.Phase,
		FailureCode: view.Code, MigrationName: marker.MigrationName, SourceSchema: marker.SourceSchema,
		TargetSchema: marker.TargetSchema, RollbackRequired: marker.RollbackRequired}
}

func (source migrationOverviewSource) applyRestoreState(ctx context.Context, view *handler.MigrationOverviewView) error {
	pending, err := backup.PendingRestore(source.dataDir)
	if err != nil {
		view.State, view.Code = handler.MigrationStateInconsistent, handler.MigrationCodeRestoreUnknown
		return err
	}
	view.RestorePending = pending
	if pending {
		view.State, view.Code = handler.MigrationStatePendingRestore, handler.MigrationCodePendingRestore
	}
	view.RestoreStaged = source.hasStagedRestore(ctx)
	return nil
}

// hasStagedRestore 在有界扫描里判断是否存在暂存中的恢复记录。
func (source migrationOverviewSource) hasStagedRestore(ctx context.Context) bool {
	if source.backups == nil {
		return false
	}
	page, err := source.backups.ListBackupsPage(ctx, store.BackupQuery{State: model.BackupStateStaged, Limit: migrationOverviewStagedScanCap})
	if err != nil {
		return false
	}
	return len(page.Items) > 0
}

// applySchemaState 读取已应用迁移版本；运行中的服务端总是停在当前二进制上限。
func (source migrationOverviewSource) applySchemaState(ctx context.Context, view *handler.MigrationOverviewView) {
	version, err := source.sqlite.SchemaVersion(ctx)
	if err != nil {
		view.ApplicationHealth = "degraded"
		view.State, view.Code = handler.MigrationStateInconsistent, handler.MigrationCodeUnreadable
		return
	}
	view.CurrentSchema = version
	if version != store.MaximumSupportedSchemaVersion {
		view.ApplicationHealth = "degraded"
		view.State = handler.MigrationStateUpgradePending
		view.Code = handler.MigrationCodeSchemaMismatch
	}
	if name := store.MigrationName(version); name != "" {
		view.LatestCompleted = &handler.MigrationStepView{Version: version, Name: name}
	}
	pending := store.PendingMigrations(version)
	steps := make([]handler.MigrationStepView, 0, len(pending))
	for _, migration := range pending {
		steps = append(steps, handler.MigrationStepView{Version: migration.Version, Name: migration.Name})
	}
	view.PendingMigrations = handler.MigrationSteps(steps)
}

func (source migrationOverviewSource) applyMetricsHealth(view *handler.MigrationOverviewView) {
	if source.mts == nil {
		return
	}
	if healthy, _ := source.mts.Health(); !healthy {
		view.MetricsHealth = "degraded"
	}
}
