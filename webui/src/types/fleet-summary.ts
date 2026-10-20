// v23 前端契约；后端启用前仅展示明确的不可用状态。
export interface FleetSummaryWrite { name: string; cadence: "daily" | "weekly"; timezone: string; local_time: string; weekday: number | null; name_threshold: number; enabled: boolean; node_ids: string[]; group_ids: string[]; channel_ids: string[] }
export interface FleetSummarySchedule extends FleetSummaryWrite { id: string; revision: number; next_run_at: string | null; last_result: string | null; delivery_state: string | null }
export interface FleetSummaryReceipt { run_id: string; status: "queued" | "success" | "partial" | "failed" }
