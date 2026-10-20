import { useEffect, useState } from "react"
import { Link } from "react-router"
import { BellRingIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { evaluationRules } from "@/lib/alert-evaluation-api"
import { replacePanelAlerts, type DashboardPanelAlert } from "@/lib/metric-collaboration-api"
import { alertHistoryLink, alertStateKey, useCollaborationText } from "./collaboration-text"
import type { EvaluationRule } from "@/types/alert-evaluation"
import type { DashboardPanel } from "@/types/metric-workspace"

/** 面板徽标只显示聚合后的最严重状态，点击下钻到该规则的评估历史。 */
export function PanelAlertBadge(props: { alerts: DashboardPanelAlert[]; context: string }) {
  const text = useCollaborationText()
  if (props.alerts.length === 0) return null
  const worst = props.alerts.reduce((left, right) => rank(right) > rank(left) ? right : left)
  const state = worst.state
  const active = (state?.firing ?? 0) + (state?.pending ?? 0)
  return <Badge variant={rank(worst) >= 4 ? "destructive" : "outline"} render={<Link to={alertHistoryLink(worst.rule_id)} />}>
    <BellRingIcon />{text(alertStateKey(state?.phase ?? "normal", state?.status ?? "ok"))}
    {active > 0 ? ` ${active}` : ""}
    <span className="sr-only">{props.context}</span>
  </Badge>
}

function rank(value: DashboardPanelAlert): number {
  const phase = value.state?.phase ?? "normal"
  const status = value.state?.status ?? "ok"
  if (phase === "firing") return 6
  if (status === "error") return 5
  if (phase === "pending") return 4
  if (status === "no_data") return 3
  if (phase === "recovering") return 2
  return 1
}

/** 关联面板与告警规则；规则本身仍由告警模块拥有，这里只写关联表。 */
export function PanelAlertDialog(props: {
  dashboardId: string; panel: DashboardPanel; alerts: DashboardPanelAlert[]
  close: () => void; applied: (values: DashboardPanelAlert[]) => void; onError: (value: string | null) => void
}) {
  const text = useCollaborationText()
  const [rules, setRules] = useState<EvaluationRule[]>([])
  const [selected, setSelected] = useState<string[]>(props.alerts.map((value) => value.rule_id))
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    evaluationRules(controller.signal).then(setRules).catch(() => { if (!controller.signal.aborted) props.onError(text("load_failed")) })
    return () => controller.abort()
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- 规则列表只拉取一次，错误回调通过闭包捕获
  }, [])
  function toggle(id: string, checked: boolean) {
    setSelected((previous) => checked ? [...previous, id].slice(0, 4) : previous.filter((value) => value !== id))
  }
  async function submit() {
    setBusy(true)
    try {
      props.applied(await replacePanelAlerts(props.dashboardId, props.panel.id, selected))
      props.close()
    } catch { props.onError(text("save_failed")) } finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) props.close() }}><DialogContent><DialogHeader>
    <DialogTitle>{text("link_alerts")}</DialogTitle><DialogDescription>{props.panel.title}</DialogDescription></DialogHeader>
    {rules.length === 0 ? <p className="text-sm text-muted-foreground">{text("no_alert_rules")}</p> :
      <ul className="max-h-72 space-y-2 overflow-y-auto">{rules.map((rule) => <li key={rule.id} className="flex items-center gap-3 rounded-md border px-3 py-2">
        <Checkbox id={`rule-${rule.id}`} checked={selected.includes(rule.id)} onCheckedChange={(checked) => toggle(rule.id, checked === true)} />
        <label htmlFor={`rule-${rule.id}`} className="min-w-0 flex-1 text-sm"><span className="font-medium">{rule.name}</span>
          <span className="block truncate text-xs text-muted-foreground">{rule.metric} · {rule.severity}</span></label>
        <Button variant="ghost" size="icon-sm" render={<Link to={alertHistoryLink(rule.id)} />} aria-label={`${text("view_alert_history")} · ${rule.name}`}><BellRingIcon /></Button>
      </li>)}</ul>}
    <DialogFooter><Button variant="outline" disabled={busy} onClick={props.close}>{text("cancel")}</Button>
      <Button disabled={busy} onClick={() => void submit()}>{text("save")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
