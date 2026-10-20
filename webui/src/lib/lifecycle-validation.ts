import type { LifecycleWrite } from "@/types/node-lifecycle"
import { priceToMicros, validCalendarDate } from "@/lib/lifecycle-format"
export function validateLifecycleDraft(draft: LifecycleWrite, price: string, previousMode: LifecycleWrite["renewal_mode"] = "manual"): LifecycleWrite {
 const bytes = new TextEncoder()
 if (bytes.encode(draft.provider).length > 100 || bytes.encode(draft.plan).length > 100 || bytes.encode(draft.private_notes).length > 2048) throw new Error("Text exceeds UTF-8 byte limit")
 if (!validCalendarDate(draft.expires_on) || !Number.isInteger(draft.interval_count) || draft.interval_count < 1 || draft.interval_count > 120) throw new Error("Invalid date or interval")
 const price_micros = priceToMicros(price)
 if (price_micros !== null && !/^[A-Z]{3}$/.test(draft.currency)) throw new Error("Invalid currency")
 if (draft.renewal_mode === "calendar_auto_advance" && previousMode !== "calendar_auto_advance" && !draft.confirm_auto_advance) throw new Error("Explicit date advancement confirmation required")
 return { provider: draft.provider, plan: draft.plan, price_micros, currency: draft.currency, interval_unit: draft.interval_unit, interval_count: draft.interval_count, expires_on: draft.expires_on, renewal_mode: draft.renewal_mode, private_notes: draft.private_notes, expiry_visibility: draft.expiry_visibility, price_visibility: draft.price_visibility, confirm_auto_advance: draft.confirm_auto_advance }
}

export function parseDayOffsets(value: string, post = false): number[] {
 if (!value.trim()) return []
 const values = value.split(",").map(item => item.trim())
 if (values.length > 366 || values.some(item => !/^\d+$/.test(item) || Number(item) > 365)) throw new Error("Invalid day offsets")
 if (post && values.some(item => Number(item) === 0)) throw new Error("Post-expiry days must be positive")
 return [...new Set(values.map(Number))].sort((a, b) => a - b)
}
