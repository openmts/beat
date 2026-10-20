import type { Catalog } from "@/context/locales"
import { publicWorkspaceId } from "@/context/public-workspace/id"
import { idAccelerator } from "@/context/id/accelerator"
import { idBase } from "@/context/id/base"
import { idFleet } from "@/context/id/fleet"
import { idMonitor } from "@/context/id/monitor"
import { idTelemetry } from "@/context/id/telemetry"
import { idTheme } from "@/context/id/theme"
import { idTrafficReport } from "@/context/id/traffic-report"

// 公共外壳只内联 en；Bahasa Indonesia 目录按需加载，保证单个访客的公共首屏仍不超过 250 KiB gzip。
export const idPublicCatalog: Catalog = {
  "id": {
    ...idAccelerator,
    ...idBase,
    ...publicWorkspaceId.id,
    ...idFleet,
    ...idMonitor,
    ...idTelemetry,
    ...idTheme,
    ...idTrafficReport,
  },
}
