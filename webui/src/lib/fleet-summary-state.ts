import type { FleetSummaryWrite } from "@/types/fleet-summary"
export const emptyFleetSummary: FleetSummaryWrite = { name: "", cadence: "daily", timezone: "UTC", local_time: "08:00", weekday: null, name_threshold: 10, enabled: true, node_ids: [], group_ids: [], channel_ids: [] }
export function validateFleetSummary(value: FleetSummaryWrite): void {
 if (!value.name.trim() || new TextEncoder().encode(value.name).length > 100 || !/^([01]\d|2[0-3]):[0-5]\d$/.test(value.local_time)) throw new Error("Invalid name or local time")
 if (!Number.isInteger(value.name_threshold) || value.name_threshold < 0 || value.name_threshold > 50 || !value.channel_ids.length) throw new Error("Choose channels and a name threshold from 0 to 50")
 if (value.cadence === "weekly" && (value.weekday === null || !Number.isInteger(value.weekday) || value.weekday < 0 || value.weekday > 6)) throw new Error("Choose a weekday")
 new Intl.DateTimeFormat("en", { timeZone: value.timezone }).format()
}
