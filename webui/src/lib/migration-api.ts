import { api } from "@/lib/api-client"
import type { MigrationOverview } from "@/types/migration-overview"

export async function getMigrationOverview(signal: AbortSignal) {
  return (await api.get<MigrationOverview>("/admin/migration", { signal })).data
}
