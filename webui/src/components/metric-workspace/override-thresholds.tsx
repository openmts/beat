import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { appendedOverrideThreshold, maximumOverrideThresholds } from "@/lib/workspace-overrides"
import { WorkspaceSelect } from "./select"
import type { DashboardOverride, PanelColor } from "@/types/metric-workspace"

type OverrideThresholds = DashboardOverride["thresholds"]

/** Grafana 的 Overrides → Thresholds：每条覆盖可以带自己的一套阈值色带（最多 4 条），留空表示沿用面板阈值。 */
export function WorkspaceOverrideThresholds(props: { index: number; thresholds: OverrideThresholds; change: (thresholds: OverrideThresholds) => void }) {
  const { t } = useLocale()
  const label = props.index + 1
  const thresholds = props.thresholds ?? []
  const edit = (current: number, patch: Partial<{ value: number; color: PanelColor }>) =>
    props.change(thresholds.map((threshold, index) => index === current ? { ...threshold, ...patch } : threshold))
  // 删掉最后一条等于回到「沿用面板阈值」：不留下服务端会当成空规则拒绝的空数组。
  const remove = (current: number) => {
    const kept = thresholds.filter((_, index) => index !== current)
    props.change(kept.length ? kept : undefined)
  }
  return <div className="w-full space-y-2 rounded-md bg-muted/40 p-2">
    <span className="text-xs font-medium">{t("workspace.override_thresholds")} {label}</span>
    {thresholds.map((threshold, current) => <div key={current} className="flex items-center gap-3">
      <Input className="max-w-44" type="number" step="any" aria-label={`${t("workspace.override_thresholds")} ${label} ${current + 1}`}
        value={Number.isFinite(threshold.value) ? threshold.value : ""}
        onChange={(event) => edit(current, { value: event.target.value === "" ? NaN : Number(event.target.value) })} />
      <WorkspaceSelect label={`${t("workspace.override_thresholds")} ${label} ${t("workspace.override_color")} ${current + 1}`} value={threshold.color}
        options={Object.keys(panelColors).map((color) => ({ value: color, label: t(`workspace.${color}`) }))}
        onChange={(color) => edit(current, { color: color as PanelColor })} />
      <Button size="icon-sm" variant="ghost" aria-label={`${t("workspace.override_thresholds")} ${label} ${t("workspace.threshold_remove")} ${current + 1}`} onClick={() => remove(current)}><XIcon /></Button>
    </div>)}
    <Button variant="outline" size="sm" aria-label={`${t("workspace.override_thresholds")} ${label} ${t("workspace.add_threshold")}`}
      disabled={thresholds.length >= maximumOverrideThresholds}
      onClick={() => props.change(appendedOverrideThreshold(thresholds))}>{t("workspace.add_threshold")}</Button>
  </div>
}
