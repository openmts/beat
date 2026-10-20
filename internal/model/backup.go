package model

import "time"

const (
	BackupStateRunning     = "running"
	BackupStateReady       = "ready"
	BackupStateFailed      = "failed"
	BackupStateValidated   = "validated"
	BackupStateStaging     = "staging"
	BackupStateStaged      = "staged"
	BackupStateInterrupted = "interrupted"
	BackupStateMissing     = "missing"
	BackupStateDeleting    = "deleting"
)

// MaximumBackupMetricRows 是备份归档与 MTS 导出共享的指标行上限：归档校验按这个上限接受
// 归档，因此导出读取预算必须不小于它，否则备份会在数据量超过读取保护阈值后直接失败。
const MaximumBackupMetricRows = 1_000_000_000

// BackupTerminalStates 是保留窗口内可达且不再自动迁移的终态。
var BackupTerminalStates = []string{
	BackupStateFailed, BackupStateInterrupted, BackupStateMissing,
}

type BackupRecord struct {
	ID             string     `json:"id"`
	Filename       string     `json:"filename"`
	Source         string     `json:"source"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	SnapshotCutoff *time.Time `json:"snapshot_cutoff"`
	SizeBytes      int64      `json:"size_bytes"`
	SQLiteBytes    int64      `json:"sqlite_bytes"`
	MetricsBytes   int64      `json:"metrics_bytes"`
	MetricRows     int64      `json:"metric_rows"`
	ErrorMessage   string     `json:"error_message"`
	// RestoreGeneration 是恢复暂存阶段生成的随机代次，只在 staging/staged 期间非空。
	RestoreGeneration string `json:"restore_generation,omitempty"`
	// PreviousState 记录暂存前的 ready/validated 状态，供回滚使用。
	PreviousState string     `json:"previous_state,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ReconciledAt  *time.Time `json:"reconciled_at"`
}

// BackupPage 只返回一页备份记录与下一页游标，绝不整表下发。
type BackupPage struct {
	Items      []BackupRecord `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// RestoreOriginOffline 标记本机运维恢复命令直接写入的 journal；在线暂存必须由
// staged 记录与其随机代次共同授权。
const RestoreOriginOffline = "offline"

// BackupQuarantineRecord 记录被移入私有隔离目录的孤儿归档，绝不自动导入或删除。
type BackupQuarantineRecord struct {
	ID            string    `json:"id"`
	Filename      string    `json:"filename"`
	SizeBytes     int64     `json:"size_bytes"`
	Reason        string    `json:"reason"`
	QuarantinedAt time.Time `json:"quarantined_at"`
}

type BackupManifest struct {
	AgentReleaseCache *BackupAgentReleaseCache   `json:"agent_release_cache,omitempty"`
	FormatVersion     int                        `json:"format_version"`
	BeatVersion       string                     `json:"beat_version"`
	CreatedAt         time.Time                  `json:"created_at"`
	SnapshotCutoff    time.Time                  `json:"snapshot_cutoff"`
	MetricRows        int64                      `json:"metric_rows"`
	CatalogVersion    int                        `json:"catalog_version,omitempty"`
	CatalogRevision   int64                      `json:"catalog_revision,omitempty"`
	PayloadSizes      map[string]int64           `json:"payload_sizes"`
	Checksums         map[string]string          `json:"checksums"`
	RequiredExternal  []string                   `json:"required_external_settings"`
	ThemeFiles        map[string]BackupThemeFile `json:"theme_files,omitempty"`
	GeoIPFiles        map[string]MMDBMetadata    `json:"geoip_files,omitempty"`
	ExternalMMDB      string                     `json:"external_mmdb,omitempty"`
}

// BackupThemeFile 记录备份中一个主题文件的路径、大小与 SHA-256。
type BackupThemeFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type RestoreJournal struct {
	BackupID       string     `json:"backup_id"`
	ArchivePath    string     `json:"archive_path"`
	State          string     `json:"state"`
	Generation     string     `json:"generation,omitempty"`
	Origin         string     `json:"origin,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	AppliedAt      *time.Time `json:"applied_at,omitempty"`
	SQLiteRollback string     `json:"sqlite_rollback,omitempty"`
	MTSRollback    string     `json:"mts_rollback,omitempty"`
}

// BackupAgentReleaseCache 记录未打包的制品；恢复后须重新验证并导入。
type BackupAgentReleaseCache struct {
	Excluded                 bool                         `json:"excluded"`
	RequiresVerifiedReimport bool                         `json:"requires_verified_reimport"`
	Artifacts                []BackupAgentReleaseArtifact `json:"artifacts"`
}

type BackupAgentReleaseArtifact struct {
	ReleaseID  string `json:"release_id"`
	ArtifactID string `json:"artifact_id"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
}
