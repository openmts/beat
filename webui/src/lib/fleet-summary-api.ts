import { api } from "@/lib/api-client"
import type { FleetSummaryWrite, FleetSummarySchedule, FleetSummaryReceipt } from "@/types/fleet-summary"
const base = "/alerts/fleet-summaries"
export async function listFleetSummaries(signal?: AbortSignal) { return (await api.get<FleetSummarySchedule[]>(base, { signal })).data }
export async function createFleetSummary(input: FleetSummaryWrite) { return (await api.post<FleetSummarySchedule>(base, input)).data }
export async function updateFleetSummary(id: string, input: FleetSummaryWrite & { expected_revision: number }) { return (await api.put<FleetSummarySchedule>(`${base}/${encodeURIComponent(id)}`, input)).data }
export async function deleteFleetSummary(id: string, expected_revision: number) { await api.delete(`${base}/${encodeURIComponent(id)}`, { data: { expected_revision } }) }
export async function testFleetSummary(id: string) { return (await api.post<FleetSummaryReceipt>(`${base}/${encodeURIComponent(id)}/test-run`)).data }
