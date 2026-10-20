import { useEffect, useState } from "react"
import { CopyIcon, HistoryIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"
import { Link } from "react-router"
import { useApiList } from "@/hooks/use-api-list"
import { useEvaluationResource } from "@/hooks/use-evaluation-resource"
import { evaluationRules, evaluationSources, deleteEvaluationRule, saveEvaluationRule } from "@/lib/alert-evaluation-api"
import { evaluationCopySeed, evaluationDraft, evaluationError, evaluationWrite } from "@/lib/alert-evaluation-form"
import { evaluationHistoryLink } from "@/lib/alert-evaluation-links"
import { workspaceAlertSeedFromQuery, type WorkspaceAlertSeed } from "@/lib/workspace-alert-seed"
import type { EvaluationRule, EvaluationWrite } from "@/types/alert-evaluation"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { EvaluationRuleEditor } from "@/pages/admin/evaluation-rule-editor"
import { useEvaluationText } from "@/pages/admin/evaluation-text"

export default function EvaluationRulesPanel() {
  const t = useEvaluationText(), rules = useApiList(evaluationRules), sources = useEvaluationResource(evaluationSources)
  const [editing, setEditing] = useState<EvaluationRule | null | undefined>(undefined), [deleting, setDeleting] = useState<EvaluationRule | null>(null)
  const [busy, setBusy] = useState(false), [error, setError] = useState<string | null>(null)
  // 新建草稿的来源：面板级告警深链或复制既有规则；编辑既有规则时不使用。
  const [draft, setDraft] = useState<Partial<EvaluationWrite> | undefined>(undefined)
  // 面板级告警入口把种子放在 URL 上；只在指标确实存在于目录时预填，否则保持默认草稿。
  const [seed, setSeed] = useState<WorkspaceAlertSeed | null>(() => workspaceAlertSeedFromQuery(new URLSearchParams(window.location.search)))
  const [seeded, setSeeded] = useState(false)
  useEffect(() => {
    if (!seed || seeded || !sources.data) return
    setSeeded(true)
    if (sources.data.metrics.some(metric => metric.key === seed.metric)) { setDraft(alertRuleSeed(seed)); setEditing(null) }
  }, [seed, seeded, sources.data])
  const open = (rule: EvaluationRule | null, next?: Partial<EvaluationWrite>) => { setDraft(next); setEditing(rule) }
  const mutate = async (rule: EvaluationRule, remove = false) => {
    setBusy(true); setError(null)
    try { if (remove) { await deleteEvaluationRule(rule); setDeleting(null) } else { await saveEvaluationRule({ ...evaluationWrite(evaluationDraft(rule)), enabled: !rule.enabled }, rule) }; await rules.refresh() }
    catch (error) { setError(evaluationError(error)) }
    finally { setBusy(false) }
  }
  return <div className="space-y-4">
    <div className="flex justify-end gap-2"><Button variant="outline" disabled={rules.loading} onClick={() => void rules.refresh()}>{t("refresh")}</Button><Button disabled={!sources.data || sources.loading || busy} onClick={() => open(null)}><PlusIcon />{t("create")}</Button></div>
    {(error || rules.error || sources.error) && <Alert variant="destructive"><AlertDescription>{t(error ?? rules.error ?? sources.error ?? "")}</AlertDescription></Alert>}
    <Card><CardContent className="p-0"><Table><TableHeader><TableRow>{["name", "metric", "scope", "threshold", "severity", "enabled", "detail"].map(key => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>
      {(rules.data ?? []).map(rule => <TableRow key={rule.id}>
        <TableCell className="font-medium">{rule.name}{rule.numeric_policy_locked && <Badge variant="outline" className="ml-2">{t("policyLocked")}</Badge>}</TableCell>
        <TableCell>{sources.data?.metrics.find(metric => metric.key === rule.metric)?.label ?? rule.metric}</TableCell><TableCell>{t(rule.scope.kind)}</TableCell>
        <TableCell className="font-mono text-xs">{rule.operator} {rule.threshold} · {rule.duration}s<br />{rule.aggregation} / {rule.window_seconds}s</TableCell>
        <TableCell><Badge variant={rule.severity === "critical" ? "destructive" : "outline"}>{rule.severity}</Badge></TableCell>
        <TableCell><Switch aria-label={`${rule.name} ${t("enabled")}`} checked={rule.enabled} disabled={busy} onCheckedChange={() => void mutate(rule)} /></TableCell>
        <TableCell><div className="flex gap-1"><Button variant="ghost" size="icon-sm" aria-label={`${t("history")} ${rule.name}`} render={<Link to={evaluationHistoryLink(rule.id)} />}><HistoryIcon /></Button><Button variant="ghost" size="icon-sm" aria-label={`${t("duplicate")} ${rule.name}`} disabled={busy} onClick={() => open(null, evaluationCopySeed(rule, t("copySuffix")))}><CopyIcon /></Button><Button variant="ghost" size="icon-sm" aria-label={`${t("edit")} ${rule.name}`} disabled={!sources.data || busy} onClick={() => open(rule)}><PencilIcon /></Button><Button variant="ghost" size="icon-sm" aria-label={`${t("remove")} ${rule.name}`} disabled={busy} onClick={() => setDeleting(rule)}><Trash2Icon className="text-destructive" /></Button></div></TableCell>
      </TableRow>)}
      {!rules.data?.length && <TableRow><TableCell colSpan={7} className="py-8 text-center text-muted-foreground">{t(rules.loading ? "loading" : "empty")}</TableCell></TableRow>}
    </TableBody></Table></CardContent></Card>
    {editing !== undefined && sources.data && <EvaluationRuleEditor rule={editing} sources={sources.data} seed={editing === null ? draft : undefined} onClose={() => { setEditing(undefined); setDraft(undefined); setSeed(null) }} onSaved={() => void rules.refresh()} />}
    <Dialog open={deleting !== null} onOpenChange={open => { if (!open && !busy) setDeleting(null) }}><DialogContent><DialogHeader><DialogTitle>{t("remove")} {deleting?.name}</DialogTitle><DialogDescription>{t("deleteHelp")}</DialogDescription></DialogHeader>{error && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}<DialogFooter><Button variant="outline" disabled={busy} onClick={() => setDeleting(null)}>{t("cancel")}</Button><Button variant="destructive" disabled={busy} onClick={() => { if (deleting) void mutate(deleting, true) }}>{t("remove")}</Button></DialogFooter></DialogContent></Dialog>
  </div>
}

/** 面板种子 → 规则写入草稿；未提供的字段沿用规则默认值。 */
function alertRuleSeed(seed: WorkspaceAlertSeed | null): Partial<EvaluationWrite> | undefined {
  if (!seed) return undefined
  return { metric: seed.metric, aggregation: seed.aggregation, window_seconds: seed.window_seconds, scope: { ...seed.scope },
    ...(seed.threshold === undefined ? {} : { threshold: seed.threshold }), ...(seed.name ? { name: seed.name } : {}) }
}
