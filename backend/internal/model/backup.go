package model

import "time"

const (
	BackupStateRunning   = "running"
	BackupStateReady     = "ready"
	BackupStateFailed    = "failed"
	BackupStateValidated = "validated"
	BackupStateStaged    = "staged"
)

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
}

type BackupManifest struct {
	FormatVersion    int               `json:"format_version"`
	BeatVersion      string            `json:"beat_version"`
	CreatedAt        time.Time         `json:"created_at"`
	SnapshotCutoff   time.Time         `json:"snapshot_cutoff"`
	MetricRows       int64             `json:"metric_rows"`
	PayloadSizes     map[string]int64  `json:"payload_sizes"`
	Checksums        map[string]string `json:"checksums"`
	RequiredExternal []string          `json:"required_external_settings"`
}

type RestoreJournal struct {
	BackupID       string     `json:"backup_id"`
	ArchivePath    string     `json:"archive_path"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	AppliedAt      *time.Time `json:"applied_at,omitempty"`
	SQLiteRollback string     `json:"sqlite_rollback,omitempty"`
	MTSRollback    string     `json:"mts_rollback,omitempty"`
}
