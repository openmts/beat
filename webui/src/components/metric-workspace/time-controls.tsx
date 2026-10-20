import { useState } from "react"
import { CalendarIcon, PauseIcon, RefreshCwIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLocale } from "@/context/locale"
import { workspaceDomain } from "@/lib/workspace-query"
import { parseWorkspaceTimeExpression } from "@/lib/workspace-time-expression"
import { WorkspaceSelect } from "./select"
import type { WorkspaceRange } from "@/types/metric-workspace"

const rangeOptions = [[300, "last5m"], [900, "last15m"], [3600, "last1h"], [21600, "last6h"], [86400, "last24h"], [604800, "last7d"], [2592000, "last30d"]] as const

export function WorkspaceTimeControls(props: {
  range: WorkspaceRange; refreshSeconds: number; onRange: (range: WorkspaceRange) => void
  onInterval: (seconds: number) => void; onRefresh: () => void
}) {
  const { t } = useLocale()
  const [custom, setCustom] = useState(false)
  const options = rangeOptions.map(([value, label]) => ({ value: String(value), label: t(`workspace.${label}`) })).concat({ value: "custom", label: t("workspace.custom_range") })
  return <div className="flex flex-wrap items-center gap-2">
    <WorkspaceSelect label={t("workspace.time_range")} value={props.range.mode === "relative" ? String(props.range.seconds) : "custom"} options={options}
      onChange={(value) => value === "custom" ? setCustom(true) : props.onRange({ mode: "relative", seconds: Number(value) })} />
    <Button variant="outline" size="icon" aria-label={t("workspace.custom_range")} onClick={() => setCustom(true)}><CalendarIcon /></Button>
    <WorkspaceSelect label={t("workspace.refresh_interval")} value={String(props.refreshSeconds)} disabled={props.range.mode === "absolute"}
      options={[0, 5, 10, 15, 30, 60, 300].map((value) => ({ value: String(value), label: value ? t("workspace.seconds", { seconds: value }) : t("workspace.off") }))}
      onChange={(value) => props.onInterval(Number(value))} />
    <Button variant="outline" size="icon" aria-label={t("workspace.pause")} disabled={!props.refreshSeconds} onClick={() => props.onInterval(0)}><PauseIcon /></Button>
    <Button variant="outline" size="icon" aria-label={t("workspace.refresh")} onClick={props.onRefresh}><RefreshCwIcon /></Button>
    {custom && <CustomTimeRange range={props.range} close={() => setCustom(false)} apply={props.onRange} />}
  </div>
}

function CustomTimeRange(props: { range: WorkspaceRange; close: () => void; apply: (range: WorkspaceRange) => void }) {
  const { t } = useLocale()
  const [domain, setDomain] = useState(() => workspaceDomain(props.range))
  const [expressions, setExpressions] = useState({ from: "", to: "" })
  const [error, setError] = useState<string | null>(null)
  // 表达式只负责把相对时间换算成 UTC 时刻，最终提交的仍是上面那两个 datetime 值。
  function applyExpression(key: "from" | "to", text: string) {
    setExpressions((previous) => ({ ...previous, [key]: text }))
    if (!text.trim()) { setError(null); return }
    const parsed = parseWorkspaceTimeExpression(text)
    if ("error" in parsed) { setError(parsed.error); return }
    setDomain((previous) => ({ ...previous, [key]: parsed.value })); setError(null)
  }
  function submit() {
    try { props.apply({ mode: "absolute", ...workspaceDomain({ mode: "absolute", ...domain }) }); props.close() }
    catch { setError("workspace.invalid_range") }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}>
    <DialogContent><DialogHeader><DialogTitle>{t("workspace.custom_range")}</DialogTitle><DialogDescription>{t("workspace.utc_hint")}</DialogDescription></DialogHeader>
      {(["from", "to"] as const).map((key) => <div key={key} className="space-y-2">
        <Label htmlFor={`time-expression-${key}`}>{t(`workspace.expression_${key}`)}</Label>
        <Input id={`time-expression-${key}`} value={expressions[key]} placeholder={t("workspace.expression_placeholder")}
          onChange={(event) => applyExpression(key, event.target.value)} />
        <Label htmlFor={`time-${key}`}>{t(`workspace.${key}`)}</Label>
        <Input id={`time-${key}`} type="datetime-local" step="1" value={domain[key].replace(/Z$/, "")} onChange={(event) => setDomain({ ...domain, [key]: `${event.target.value}Z` })} /></div>)}
      {error && <p role="alert" className="text-sm text-destructive">{t(error)}</p>}
      <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button><Button onClick={submit}>{t("workspace.apply")}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}
