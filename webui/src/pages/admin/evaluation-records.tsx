import { useCallback, useState } from "react"
import { useLocation } from "react-router"
import { useApiList } from "@/hooks/use-api-list"
import { useEvaluationResource } from "@/hooks/use-evaluation-resource"
import { evaluationEvents, evaluationRules, evaluationSources, evaluationStates } from "@/lib/alert-evaluation-api"
import type { EvaluationEvent, EvaluationPage, EvaluationState } from "@/types/alert-evaluation"
import { AdminSelectField } from "@/components/admin-select-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { EvaluationDetailDialog } from "@/pages/admin/evaluation-detail"
import { evaluationFiltersFromQuery, type EvaluationFilters } from "@/lib/alert-evaluation-links"
import { evaluationTime } from "@/lib/alert-evaluation-format"
import { useEvaluationText } from "@/pages/admin/evaluation-text"

type RecordItem = EvaluationState | EvaluationEvent
export function EvaluationRecords({ mode, onIncident }: { mode: "states" | "history"; onIncident?: () => void }) {
  const t = useEvaluationText(), rules = useApiList(evaluationRules), sources = useEvaluationResource(evaluationSources)
  // 规则行深链把目标规则放在 URL 上；页签切换会重挂载本组件，这里按路由地址取一次筛选。
  const { search } = useLocation()
  const [filters, setFilters] = useState<EvaluationFilters>(() => evaluationFiltersFromQuery(new URLSearchParams(search)))
  const [cursors, setCursors] = useState<string[]>([""]), [selected, setSelected] = useState<RecordItem | null>(null)
  const query = new URLSearchParams({ ...filters, cursor: cursors[cursors.length - 1], limit: "50" }).toString()
  const fetcher = useCallback((signal: AbortSignal): Promise<EvaluationPage<RecordItem>> => mode === "states" ? evaluationStates(query, signal) : evaluationEvents(query, signal), [mode, query])
  const records = useEvaluationResource(fetcher)
  const change = (key: keyof EvaluationFilters, value: string) => { setFilters(previous => ({ ...previous, [key]: value === "*" ? "" : value })); setCursors([""]) }
  const ruleNames = new Map((rules.data ?? []).map(rule => [rule.id, rule.name])), nodeNames = new Map((sources.data?.nodes ?? []).map(node => [node.id, node.name]))
  const options = { rules: [...ruleNames].map(([value, label]) => ({ value, label })), nodes: [...nodeNames].map(([value, label]) => ({ value, label })) }
  return <div className="space-y-4">
    <EvaluationRecordFilters mode={mode} filters={filters} options={options} change={change} />
    <div className="flex justify-end"><Button variant="outline" disabled={records.loading} onClick={() => void records.refresh()}>{t("refresh")}</Button></div>
    {(records.error || rules.error || sources.error) && <Alert variant="destructive"><AlertDescription>{records.error ?? rules.error ?? sources.error}</AlertDescription></Alert>}
    <Card><CardContent className="p-0"><Table><TableHeader><TableRow>{(mode === "states" ? ["rule", "node", "phase", "status", "progress", "lastGood", "detail"] : ["rule", "node", "kind", "occurred", "reason", "detail"]).map(key => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>
      {(records.data?.items ?? []).map(item => <EvaluationRecordRow key={"id" in item ? item.id : `${item.rule_id}:${item.node_id}:${item.device_id ?? ""}`} item={item} ruleName={ruleNames.get(item.rule_id) ?? item.rule_id} nodeName={nodeNames.get(item.node_id) ?? item.node_id} onOpen={() => setSelected(item)} />)}
      {!records.data?.items.length && <TableRow><TableCell colSpan={mode === "states" ? 7 : 6} className="py-8 text-center text-muted-foreground">{t(records.loading ? "loading" : "empty")}</TableCell></TableRow>}
    </TableBody></Table></CardContent></Card>
    <div className="flex justify-end gap-2"><Button variant="outline" disabled={records.loading || cursors.length === 1} onClick={() => setCursors(previous => previous.slice(0, -1))}>{t("previous")}</Button><Button variant="outline" disabled={records.loading || !records.data?.next_cursor} onClick={() => { if (records.data?.next_cursor) setCursors(previous => [...previous, records.data!.next_cursor!]) }}>{t("next")}</Button></div>
    {selected && <EvaluationDetailDialog ruleID={selected.rule_id} nodeID={selected.node_id} deviceID={selected.device_id} onClose={() => setSelected(null)} onIncident={onIncident} />}
  </div>
}
function EvaluationRecordFilters({ mode, filters, options, change }: { mode: "states" | "history"; filters: EvaluationFilters; options: { rules: Array<{ value: string; label: string }>; nodes: Array<{ value: string; label: string }> }; change: (key: keyof EvaluationFilters, value: string) => void }) {
  const t = useEvaluationText(), any = { value: "*", label: t("any") }
  const field = (key: keyof EvaluationFilters, label: string, values: Array<{ value: string; label: string }>) => <AdminSelectField id={`evaluation-filter-${key}`} label={t(label)} options={[any, ...values]} value={filters[key] || "*"} onValueChange={value => change(key, value)} />
  const labels = (values: string[]) => values.map(value => ({ value, label: t(value) }))
  return <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">{field("rule_id", "rule", options.rules)}{field("node_id", "node", options.nodes)}{mode === "states" ? <>{field("phase", "phase", labels(["normal", "pending", "firing", "recovering"]))}{field("status", "status", labels(["ok", "no_data", "error", "disabled", "out_of_scope"]))}</> : field("kind", "kind", labels(["no_data", "error", "recovered"]))}</div>
}
function EvaluationRecordRow({ item, ruleName, nodeName, onOpen }: { item: RecordItem; ruleName: string; nodeName: string; onOpen: () => void }) {
  const t = useEvaluationText()
  return <TableRow><TableCell>{ruleName}</TableCell><TableCell>{nodeName}{item.device_id && <p className="text-xs text-muted-foreground">{item.device_name || "GPU"}</p>}</TableCell>{"phase" in item ? <>
    <TableCell><Badge variant="outline">{t(item.phase)}</Badge></TableCell><TableCell><Badge variant={item.evaluation_status === "error" ? "destructive" : "secondary"}>{t(item.evaluation_status)}</Badge><p className="text-xs text-muted-foreground">{item.reason_code}</p></TableCell><TableCell className="font-mono text-xs"><div>{t("triggerProgress")}: {item.trigger_elapsed_ms / 1000}s</div><div>{t("recoveryProgress")}: {item.recovery_elapsed_ms / 1000}s</div></TableCell><TableCell>{evaluationTime(item.last_good_at)}</TableCell>
  </> : <><TableCell>{t(item.kind)}</TableCell><TableCell>{evaluationTime(item.occurred_at)}</TableCell><TableCell>{item.reason_code}</TableCell></>}
    <TableCell><Button variant="ghost" size="sm" onClick={onOpen}>{t("detail")}</Button></TableCell></TableRow>
}
