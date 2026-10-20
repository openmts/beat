import type { Catalog } from "@/context/locales"
import { publicWorkspaceZhTW } from "@/context/public-workspace/zh-tw"
import { zhTWAccelerator } from "@/context/zh-tw/accelerator"
import { zhTWBase } from "@/context/zh-tw/base"
import { zhTWFleet } from "@/context/zh-tw/fleet"
import { zhTWMonitor } from "@/context/zh-tw/monitor"
import { zhTWTelemetry } from "@/context/zh-tw/telemetry"
import { zhTWTheme } from "@/context/zh-tw/theme"
import { zhTWTrafficReport } from "@/context/zh-tw/traffic-report"

// 公共外壳只内联 en；繁體中文目录按需加载，保证单个访客的公共首屏仍不超过 250 KiB gzip。
export const zhTWPublicCatalog: Catalog = {
  "zh-TW": {
    ...zhTWBase,
    ...publicWorkspaceZhTW["zh-TW"],
    ...zhTWMonitor,
    ...zhTWTelemetry,
    ...zhTWTrafficReport,
    ...zhTWTheme,
    ...zhTWFleet,
    ...zhTWAccelerator,
  },
}
