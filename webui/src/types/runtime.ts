export interface RuntimeWorker {
  name: string
  configured: boolean
  started: boolean
  running: boolean
  state: string
  failures: number
  consecutive_failures: number
  restarts: number
  interval_seconds: number
  scheduler_lag_seconds: number
  started_at: string
  stopped_at: string
  last_attempt: string
  last_success: string
  last_failure: string
}

export interface RuntimeStream {
  generation: number
  timestamp: string
  age_seconds: number
  stale: boolean
  available: boolean
  connections: number
  reserved: number
  tracked_ips: number
  builds: number
  failures: number
  dropped: number
  build_duration_seconds: number
  skipped_cadences: number
  closed: boolean
}

export interface RuntimeStatus {
  server_time: string
  stopping: boolean
  workers: RuntimeWorker[]
  metrics: RuntimeStream
  ingress?: IngressSnapshot
}

export interface IngressLimits {
  global_rate_per_second: number
  global_burst: number
  global_in_flight: number
  identity_rate_per_second: number
  identity_burst: number
  identity_in_flight: number
}

export interface IngressReason {
  reason: string
  count: number
}

export interface IngressClass {
  class: string
  admitted: number
  rejected: number
  reasons: IngressReason[]
  in_flight: number
  identity_entries: number
  limits: IngressLimits
}

export interface IngressSnapshot {
  identity_entries: number
  identity_entries_limit: number
  identity_ttl_seconds: number
  classes: IngressClass[]
}
