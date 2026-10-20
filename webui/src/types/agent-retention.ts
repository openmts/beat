// A8 发布对象保留期前端契约：后台可配置的 rollout/release/artifact 保留窗口，
// 未显式配置时由服务端启动环境变量缺省决定。
export interface AgentRetentionDefaults {
  rollout_days: number
  release_days: number
  enabled: boolean
}

export interface AgentRetentionRunStatus {
  last_run_at?: string | null
  last_status: string
  last_error: string
  last_rollouts: number
  last_releases: number
  last_artifacts: number
  last_trigger: string
}

export interface AgentRetentionSettings {
  configured: boolean
  rollout_days: number
  release_days: number
  updated_at: string
  environment: AgentRetentionDefaults
  status: AgentRetentionRunStatus
}
