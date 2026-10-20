import type { Catalog } from "@/context/locales"
import { publicWorkspaceJa } from "@/context/public-workspace/ja"
import { jaAccelerator } from "@/context/ja/accelerator"
import { jaBase } from "@/context/ja/base"
import { jaFleet } from "@/context/ja/fleet"
import { jaMonitor } from "@/context/ja/monitor"
import { jaTelemetry } from "@/context/ja/telemetry"
import { jaTheme } from "@/context/ja/theme"
import { jaTrafficReport } from "@/context/ja/traffic-report"

// 公共外壳只内联 en；日本語目录按需加载，保证单个访客的公共首屏仍不超过 250 KiB gzip。
export const jaPublicCatalog: Catalog = {
  "ja": {
    ...jaAccelerator,
    ...jaBase,
    ...publicWorkspaceJa.ja,
    ...jaFleet,
    ...jaMonitor,
    ...jaTelemetry,
    ...jaTheme,
    ...jaTrafficReport,
  },
}
