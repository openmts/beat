export type BackupState = "running" | "ready" | "failed" | "validated" | "staged"

export interface BackupRecord {
  id: string
  filename: string
  source: "generated" | "uploaded"
  state: BackupState
  created_at: string
  completed_at: string | null
  snapshot_cutoff: string | null
  size_bytes: number
  sqlite_bytes: number
  metrics_bytes: number
  metric_rows: number
  error_message: string
}
