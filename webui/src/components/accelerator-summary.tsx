import { evaluationTime } from "@/lib/alert-evaluation-format"
import { Badge } from "@/components/ui/badge"
import { formatBytes } from "@/lib/metric-format"
import { validAcceleratorValue } from "@/lib/accelerator-metrics"
import { acceleratorDeviceName, acceleratorStale, highestUtilization } from "@/lib/accelerator-public"
import { useLocale } from "@/context/locale"
import { useAcceleratorText } from "@/pages/accelerator-text"
import type { AcceleratorDevicePublic } from "@/types/accelerator"
export function AcceleratorSummary({ devices }: { devices?: AcceleratorDevicePublic[] }) {
 const { locale } = useLocale(), t = useAcceleratorText()
 if (!devices?.length) return null
 const device = highestUtilization(devices) ?? devices[0], metrics = device.metrics
 const used = metrics.gpu_memory_used_bytes, total = metrics.gpu_memory_total_bytes, utilization = metrics.gpu_utilization_percent
 return <div className="min-w-0 space-y-1 rounded-md border px-2.5 py-2 text-xs" aria-label={t("current")}>
  <div className="flex items-center justify-between gap-2"><span className="truncate font-medium" title={acceleratorDeviceName(device)}>{devices.length === 1 ? acceleratorDeviceName(device) : `${devices.length} GPUs`}</span>{devices.some(value => acceleratorStale(value)) && <Badge variant="outline">{t("staleOrMissing")}</Badge>}</div>
  <div className="flex flex-wrap justify-between gap-2 font-mono tabular-nums"><span>{devices.length > 1 ? `${t("peakPrefix")} ` : ""}{!validAcceleratorValue("gpu_utilization_percent", utilization) ? "—" : `${utilization.toFixed(1)}%`}</span><span>{!validAcceleratorValue("gpu_memory_used_bytes", used) || !validAcceleratorValue("gpu_memory_total_bytes", total) || used > total ? "—" : `${formatBytes(used)} / ${formatBytes(total)}`}</span></div>
  {device.captured_at && <p className="truncate text-muted-foreground" title={device.captured_at}>{`${t("sampled")} `}{evaluationTime(device.captured_at, locale)}</p>}
 </div>
}
