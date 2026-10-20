import { useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { AdminSelectField } from "@/components/admin-select-field"
import { evaluationDraft, evaluationError, evaluationWrite } from "@/lib/alert-evaluation-form"
import { saveEvaluationRule, type EvaluationSources } from "@/lib/alert-evaluation-api"
import type { EvaluationRule, EvaluationWrite } from "@/types/alert-evaluation"
import { EvaluationHealthFields, EvaluationNumericFields, EvaluationScopeFields } from "@/pages/admin/evaluation-rule-fields"
import { EvaluationPreview } from "@/pages/admin/evaluation-preview"
import { useEvaluationText } from "@/pages/admin/evaluation-text"

interface Props { rule: EvaluationRule | null; sources: EvaluationSources; seed?: Partial<EvaluationWrite>; onClose: () => void; onSaved: () => void }
export function EvaluationRuleEditor({ rule, sources, seed, onClose, onSaved }: Props) {
  const t = useEvaluationText(), [draft, setDraft] = useState(() => evaluationDraft(rule ?? undefined, seed))
  const [busy, setBusy] = useState(false), [error, setError] = useState<string | null>(null)
  const save = async () => {
    setBusy(true); setError(null)
    try { await saveEvaluationRule(evaluationWrite(draft), rule); onSaved(); onClose() }
    catch (error) { setError(evaluationError(error)) }
    finally { setBusy(false) }
  }
  const fields = { draft, sources, locked: rule?.numeric_policy_locked ?? false, onChange: setDraft }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}><DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
    <DialogHeader><DialogTitle>{t(rule ? "edit" : "create")}</DialogTitle></DialogHeader>
    {error && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}
    {fields.locked && <Alert><AlertDescription>{t("locked")}</AlertDescription></Alert>}
    <fieldset disabled={busy} className="space-y-4">
      <Field><FieldLabel htmlFor="evaluation-name">{t("name")}</FieldLabel><Input id="evaluation-name" maxLength={128} value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} /></Field>
      <Field><FieldLabel htmlFor="evaluation-description">{t("description")}</FieldLabel><Input id="evaluation-description" maxLength={2048} value={draft.description} onChange={event => setDraft({ ...draft, description: event.target.value })} /></Field>
      <EvaluationNumericFields {...fields} /><EvaluationScopeFields {...fields} />
      <AdminSelectField id="evaluation-severity" label={t("severity")} value={draft.severity} options={["info", "warning", "critical"].map(value => ({ value, label: value }))} onValueChange={severity => setDraft({ ...draft, severity })} />
      <EvaluationHealthFields {...fields} /><EvaluationPreview draft={draft} nodes={sources.nodes} />
    </fieldset>
    <DialogFooter><Button variant="outline" disabled={busy} onClick={onClose}>{t("cancel")}</Button><Button disabled={busy} onClick={() => void save()}>{t("save")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
