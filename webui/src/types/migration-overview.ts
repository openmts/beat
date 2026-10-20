export interface MigrationStep {
  version: number
  name: string
}

export interface MigrationFailure {
  updated_at: string
  state: string
  phase: string
  failure_code: string
  migration_name: string
  source_schema: number
  target_schema: number
  rollback_required: boolean
}

export interface MigrationOverview {
  version: string
  state: "current" | "upgrade_pending" | "pending_restore" | "inconsistent" | string
  code: string
  runbook_hash: string
  application_health: string
  metrics_health: string
  current_schema: number
  minimum_supported_schema: number
  maximum_supported_schema: number
  latest_completed?: MigrationStep
  pending_migrations: MigrationStep[]
  restore_pending: boolean
  restore_staged: boolean
  last_failure?: MigrationFailure
}
