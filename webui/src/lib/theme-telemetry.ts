import { publicText, ThemeDataError } from "@/lib/theme-json"
import type { Node } from "@/types"
import type { ThemeNode } from "@/types/theme-runtime"

export function themeTelemetry(node: Node): Pick<ThemeNode, "telemetry_at" | "telemetry_age_seconds" | "telemetry_state"> {
  const state = node.telemetry_state, at = node.telemetry_at, age = node.telemetry_age_seconds
  if (state === undefined && at === undefined && age === undefined) return {}
  if (state === "unavailable" && at === null && age === null) return { telemetry_state: state, telemetry_at: null, telemetry_age_seconds: null }
  if (!["fresh", "delayed", "stale"].includes(state ?? "") || typeof at !== "string" || !Number.isFinite(Date.parse(at)) || !Number.isSafeInteger(age) || age! < 0) throw new ThemeDataError("invalid_data")
  return { telemetry_state: state, telemetry_at: publicText(at), telemetry_age_seconds: age }
}
