export type RuntimeCaptureKind = "cpu" | "trace" | "heap" | "allocs" | "goroutine" | "mutex" | "block" | "threadcreate"

export interface RuntimeCapture {
  id: string
  kind: string
  status: string
  failure_code?: string
  started_at: string
  deadline: string
  expires_at: string
  bytes: number
}

export interface RuntimeCaptureState {
  active: number
  busy: boolean
  degraded: boolean
}

export interface RuntimeSummary {
  version: string
  commit: string
  go_version: string
  started_at: string
  uptime_seconds: number
  readiness: string
  capture: RuntimeCaptureState
  memory: { heap_alloc_bytes: number; heap_inuse_bytes: number; stack_inuse_bytes: number; system_bytes: number }
  scheduler: { gomaxprocs: number; goroutines: number; os_threads: number; cgo_calls: number }
  gc: { cycles: number; pause_total_seconds: number; last_pause_seconds: number }
}

export interface RuntimeCapturePage {
  captures: RuntimeCapture[]
  busy: boolean
}
