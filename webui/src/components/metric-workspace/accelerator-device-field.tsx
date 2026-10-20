import { Button } from "@/components/ui/button"
import { useAuth } from "@/context/auth"
import { useAcceleratorInventory } from "@/hooks/use-accelerator-inventory"
import { acceleratorDeviceName } from "@/lib/accelerator-public"
import { useAcceleratorText } from "@/pages/accelerator-text"
import { WorkspaceSelect } from "./select"
import type { DashboardSeries } from "@/types/metric-workspace"
export function AcceleratorDeviceField({ series, change }: { series: DashboardSeries; change: (deviceID: string | undefined) => void }) {
 const { principal } = useAuth(), t = useAcceleratorText(), fixedNode = Boolean(series.node_id && !series.node_id.startsWith("$"))
 const inventory = useAcceleratorInventory(fixedNode ? series.node_id : "", `${principal?.user.id}/${principal?.session.id}`)
 if (!fixedNode) return <p role="status" className="text-xs text-muted-foreground">{t("fixedNode")}</p>
 if (inventory.error) return <p role="alert" className="text-xs text-destructive">{inventory.error}<Button variant="link" size="sm" onClick={inventory.reload}>{t("retry")}</Button></p>
 const options = (inventory.data?.devices ?? []).filter(device => device.capabilities.includes(series.metric)).map(device => ({ value: device.device_id, label: `${acceleratorDeviceName(device)}${device.inactive_at ? ` · ${t("inactive")}` : ""}` }))
 if (series.device_id && !options.some(option => option.value === series.device_id)) options.unshift({ value: series.device_id, label: t(inventory.loading ? "missing" : "inactive") })
 options.unshift({ value: "choose", label: t("choose") })
 return <div className="space-y-2"><WorkspaceSelect label={`${t("device")} ${series.ref_id}`} value={series.device_id || "choose"} options={options} disabled={inventory.loading} onChange={value => change(value === "choose" ? undefined : value)} />
  {inventory.data && inventory.data.state !== "ok" && <p role="status" className="text-xs text-muted-foreground">{t(inventory.data.state)}{inventory.data.error_code ? ` · ${inventory.data.error_code}` : ""}</p>}
 </div>
}
