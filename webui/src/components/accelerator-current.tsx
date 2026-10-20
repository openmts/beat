import { evaluationTime } from "@/lib/alert-evaluation-format"
import { useEffect, useState } from "react"
import { acceleratorStale } from "@/lib/accelerator-public"
import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { useAcceleratorText } from "@/pages/accelerator-text"
import { acceleratorMetricKeys, acceleratorMetricLabel, validAcceleratorValue } from "@/lib/accelerator-metrics"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import type { AcceleratorDevicePublic } from "@/types/accelerator"
import type { MetricDefinition } from "@/types/metric-workspace"
export function AcceleratorCurrent({ device, catalog }: { device: AcceleratorDevicePublic; catalog?: MetricDefinition[] }) {
 const t = useAcceleratorText(), { locale } = useLocale()
 const [now, setNow] = useState(Date.now())
 useEffect(() => { const timer = setInterval(() => { if (!document.hidden) setNow(Date.now()) }, 5000); return () => clearInterval(timer) }, [])
 const keys = acceleratorMetricKeys.filter(key => catalog === undefined || catalog.some(metric => metric.key === key && metric.enabled))
 const missing = keys.some(key => device.capabilities.includes(key) && !validAcceleratorValue(key, device.metrics[key]))
 return <div className="space-y-3"><div className="flex flex-wrap gap-2"><Badge variant="outline">{t(!device.captured_at ? "missing" : acceleratorStale(device, now) ? "stale" : "fresh")}</Badge>{missing && <Badge variant="outline">{t("partial")}</Badge>}<time className="text-xs text-muted-foreground" dateTime={device.captured_at} title={device.captured_at}>{evaluationTime(device.captured_at, locale)}</time></div>
  <dl className="grid grid-cols-2 gap-3 xl:grid-cols-3">{keys.map(key => {
   const definition = catalog?.find(metric => metric.key === key), unit = definition?.unit ?? (key.includes("bytes") ? "bytes" : key.includes("percent") ? "percent" : key.includes("celsius") ? "celsius" : "watts")
   const value = device.metrics[key], supported = device.capabilities.includes(key), valid = validAcceleratorValue(key, value)
   return <div key={key} className="rounded-md border p-3"><dt className="text-xs text-muted-foreground">{definition?.label || acceleratorMetricLabel(key, t)}</dt><dd className="mt-1 font-mono text-lg tabular-nums">{supported && valid ? formatWorkspaceValue(value, unit, { locale }) : "—"}</dd>{!supported && <p className="text-xs text-muted-foreground">{t("unsupportedMetric")}</p>}</div>
  })}</dl>
 </div>
}
