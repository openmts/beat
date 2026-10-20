export type BackupState =
  | "running"
  | "ready"
  | "failed"
  | "validated"
  | "staging"
  | "staged"
  | "interrupted"
  | "missing"
  | "deleting"

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
  restore_generation?: string
  previous_state?: string
  updated_at: string
  reconciled_at: string | null
}

export interface BackupPage {
  items: BackupRecord[]
  next_cursor?: string
}

export type BackupUploadState = "open" | "quarantined" | "validated" | "cancelled" | "expired"

export interface BackupUploadSession {
  id: string
  owner_user_id: string
  filename: string
  declared_bytes: number
  declared_digest: string
  chunk_bytes: number
  chunk_count: number
  received_bytes: number
  received_chunks: number
  state: BackupUploadState
  backup_id?: string
  last_error?: string
  expires_at: string
  created_at: string
  updated_at: string
  maximum_parallel_chunks: number
  received_indexes: number[]
}

export interface BackupUploadChunk {
  session_id: string
  index: number
  offset: number
  size_bytes: number
  digest: string
  created_at: string
}

export interface BackupUploadDeclaration {
  filename: string
  size_bytes: number
  digest: string
}

export interface BackupQuarantineRecord {
  id: string
  filename: string
  size_bytes: number
  reason: string
  quarantined_at: string
}

export interface BackupQuarantineInventory {
  items: BackupQuarantineRecord[]
  total: number
}
