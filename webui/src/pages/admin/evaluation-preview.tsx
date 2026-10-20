import { useEffect, useRef, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import type { EvaluationDraft } from "@/lib/alert-evaluation-form"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { evaluationError, evaluationWrite } from "@/lib/alert-evaluation-form"
import { previewEvaluation } from "@/lib/alert-evaluation-api"
import type { EvaluationPreview as PreviewResult } from "@/types/alert-evaluation"
import type { Node } from "@/types"
import { EvaluationTargets } from "@/pages/admin/evaluation-targets"
import { useEvaluationText } from "@/pages/admin/evaluation-text"

export function EvaluationPreview({ draft, nodes }: { draft: EvaluationDraft; nodes: Node[] }) {
  const { locale } = useLocale(), t = useEvaluationText(), request = useRef<AbortController | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [result, setResult] = useState<PreviewResult[] | null>(null)
  const [busy, setBusy] = useState(false), [error, setError] = useState<string | null>(null)
  useEffect(() => { request.current?.abort(); setResult(null); setError(null); setBusy(false); return () => request.current?.abort() }, [draft, selected])
  const preview = async () => {
    request.current?.abort(); const controller = new AbortController(); request.current = controller
    setBusy(true); setError(null); setResult(null)
    try { const response = await previewEvaluation(evaluationWrite(draft), selected, controller.signal); if (!controller.signal.aborted) setResult(response) }
    catch (error) { if (!controller.signal.aborted) setError(evaluationError(error)) }
    finally { if (!controller.signal.aborted) setBusy(false) }
  }
  const eligible = nodes.filter(node => draft.scope.kind === "all" || draft.scope.kind === "nodes" && draft.scope.node_ids?.includes(node.id) || draft.scope.kind === "groups" && draft.scope.group_ids?.includes(node.group_id))
  return <section className="space-y-3 border-t pt-4">
    <h3 className="font-medium">{t("previewNodes")}</h3><p className="text-sm text-muted-foreground">{t("previewHelp")}</p>
    <EvaluationTargets items={eligible} value={selected} limit={8} onChange={setSelected} />
    <Button variant="outline" disabled={busy || selected.length === 0} onClick={() => void preview()}>{busy ? t("loading") : t("preview")}</Button>
    {error && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}
    {result?.length === 0 && <p role="status" className="text-sm text-muted-foreground">{t("emptyTargets")}</p>}
    {result && <ul className="space-y-2 text-sm">{result.map(item => <li key={`${item.node_id}:${item.device_id ?? ""}`} className="rounded-md border p-3"><strong>{nodes.find(node => node.id === item.node_id)?.name ?? item.node_id}</strong>{item.device_id && <span className="ml-2">{item.device_name || "GPU"}</span>}<span className="ml-2">{t(item.evaluation_status)}</span><span className="ml-2 font-mono">{item.value == null ? "—" : item.value}</span><p className="text-xs text-muted-foreground">{item.reason_code || (item.source_at ? formatDateTime(locale, item.source_at) : t("unknown"))}</p></li>)}</ul>}
  </section>
}
