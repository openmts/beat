import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Card, CardContent } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { approveOperationSnippet, deleteOperationSnippet, getOperationSnippetContent, getOperationSnippets, operationEncode, saveOperationSnippet } from "@/lib/operations-api"
import type { OperationSnippet } from "@/types/operations"
import { operationMutationError } from "./operation-mutation-error"
import { OperationConfirm } from "./operation-confirm"
export interface OperationSnippetDraft { snippet: OperationSnippet; content: string }
const fresh = (owner_id: string): OperationSnippetDraft => ({ snippet: { owner_id, id: "", name: "", description: "", visibility: "private", shell_family: "sh", version: 0, fingerprint: "", approved: false }, content: "" })
function validDraft(draft: OperationSnippetDraft) {
 const bytes = (value: string) => new TextEncoder().encode(value).length
 return Boolean(draft.snippet.name.trim() && draft.content.trim()) && bytes(draft.snippet.name) <= 200 && bytes(draft.snippet.description) <= 2000 && bytes(draft.content) <= 65536 && !draft.content.includes("\0")
}
export function OperationSnippets({ execute }: { execute: (value: OperationSnippetDraft) => void }) {
 const { principal } = useAuth(), { t } = useLocale()
 if (!principal?.user.id) return <p role="status">{t("operations.sign_in_with_an_administrator_account")}</p>
 return <SnippetLibrary key={`${principal.user.id}:${principal.user.role}`} userID={principal.user.id} owner={principal.user.role === "owner"} execute={execute} />
}
function SnippetLibrary({ userID, owner, execute }: { userID: string; owner: boolean; execute: (value: OperationSnippetDraft) => void }) {
 const { t } = useLocale(), resource = useOperationResource(getOperationSnippets), flow = useConfirmedOperation()
 const [draft, setDraft] = useState<OperationSnippetDraft | null>(null), lifecycle = useRef<{ generation: number; mounted: boolean; read: AbortController | null }>({ generation: 0, mounted: true, read: null })
 useEffect(() => { const active = lifecycle.current; active.mounted = true; return () => { active.mounted = false; active.generation++; active.read?.abort() } }, [])
 const clear = () => { lifecycle.current.generation++; lifecycle.current.read?.abort(); lifecycle.current.read = null; setDraft(null); flow.close() }
 const read = (snippet: OperationSnippet, run: boolean) => {
  if (flow.busy || run && (!snippet.approved || snippet.visibility !== "shared")) return
  clear(); const expected = ++lifecycle.current.generation, pinned = structuredClone(snippet)
  flow.request({ title: t("operations.read_snippet_content"), description: `${pinned.name} · v${pinned.version}`, execute: async () => {
   if (!lifecycle.current.mounted || lifecycle.current.generation !== expected) return
   const controller = new AbortController(); lifecycle.current.read = controller
   let content: string
   try { content = await getOperationSnippetContent(pinned, controller.signal) }
   finally { if (lifecycle.current.read === controller) lifecycle.current.read = null }
   if (controller.signal.aborted || !lifecycle.current.mounted || lifecycle.current.generation !== expected) return
   const value = { snippet: pinned, content }; if (run) execute(value); else setDraft(value)
  } })
 }
 const save = () => {
  if (!draft || !validDraft(draft) || draft.snippet.id && draft.snippet.owner_id !== userID) return
  const { id, name, description, visibility, shell_family, version } = draft.snippet, expected = lifecycle.current.generation
  const input = { id, name, description, visibility, shell_family, expected_version: version, content: operationEncode(draft.content) }
  flow.request({ requiresRecent: false, title: t("operations.save_snippet_version"), description: t("operations.the_new_shared_version_requires_owner_approval_b"), execute: async () => { if (!lifecycle.current.mounted || lifecycle.current.generation !== expected) return; try { await saveOperationSnippet(input) } catch (error) { throw operationMutationError(error, t("operations.snippet_version_changed_your_draft_is_preserved")) }; if (lifecycle.current.mounted && lifecycle.current.generation === expected) { setDraft(null); lifecycle.current.generation++; resource.reload() } } })
 }
 const approve = (snippet: OperationSnippet) => { const pinned = structuredClone(snippet); flow.request({ title: t("operations.approve_this_version"), description: `${pinned.name} · v${pinned.version}`, execute: async () => { if (!lifecycle.current.mounted) return; try { await approveOperationSnippet(pinned) } catch (error) { throw operationMutationError(error, t("operations.snippet_version_changed_reload_and_review_the_cu")) }; if (lifecycle.current.mounted) resource.reload() } }) }
 const remove = (snippet: OperationSnippet) => flow.request({ requiresRecent: false, title: t("operations.delete_snippet"), description: snippet.name, execute: async () => { if (!lifecycle.current.mounted) return; await deleteOperationSnippet(snippet.id); if (lifecycle.current.mounted) { resource.reload(); if (draft?.snippet.id === snippet.id) { setDraft(null); lifecycle.current.generation++ } } } })
 return <section className="space-y-4"><div className="flex items-center justify-between"><h2 className="font-semibold">{t("operations.snippet_library")}</h2><div className="flex gap-2"><Button variant="outline" disabled={resource.loading || flow.busy} onClick={resource.reload}>{t("operations.refresh")}</Button><Button disabled={flow.busy} onClick={() => { clear(); setDraft(fresh(userID)) }}>{t("operations.new_snippet")}</Button></div></div>
  {resource.error && <p role="alert" className="text-destructive">{resource.error}</p>}
  {!resource.error && !resource.data?.length && <p role="status">{resource.loading ? (t("operations.loading")) : (t("operations.no_snippets"))}</p>}
  {resource.data?.map(snippet => <SnippetRow key={snippet.id} snippet={snippet} author={snippet.owner_id === userID} owner={owner} busy={flow.busy} read={read} approve={approve} remove={remove} />)}
  {draft && <SnippetEditor draft={draft} change={setDraft} readOnly={Boolean(draft.snippet.id && draft.snippet.owner_id !== userID)} busy={flow.busy} clear={clear} save={save} />}<OperationConfirm flow={flow} />
 </section>
}
interface RowProps { snippet: OperationSnippet; author: boolean; owner: boolean; busy: boolean; read: (snippet: OperationSnippet, run: boolean) => void; approve: (snippet: OperationSnippet) => void; remove: (snippet: OperationSnippet) => void }
function SnippetRow({ snippet, author, owner, busy, read, approve, remove }: RowProps) {
 const { t } = useLocale()
 return <Card><CardContent className="flex flex-wrap items-center gap-2 pt-4"><div className="min-w-40 flex-1"><p className="font-medium">{snippet.name}</p><p className="text-xs text-muted-foreground">v{snippet.version} · {snippet.shell_family} · {snippet.approved ? (t("operations.approved")) : (t("operations.pending_approval"))}</p><p className="mt-1 text-sm text-muted-foreground">{snippet.description}</p><p className="break-all font-mono text-xs text-muted-foreground">{snippet.fingerprint}</p></div>
  <Button variant="outline" disabled={busy} onClick={() => read(snippet, false)}>{author ? (t("operations.view_edit")) : (t("operations.view"))}</Button><Button disabled={busy || !snippet.approved || snippet.visibility !== "shared"} onClick={() => read(snippet, true)}>{t("operations.dispatch")}</Button>
  {owner && snippet.visibility === "shared" && !snippet.approved && <Button variant="outline" disabled={busy} onClick={() => approve(snippet)}>{t("operations.approve")}</Button>}
  {author && <Button variant="ghost" disabled={busy} onClick={() => remove(snippet)}>{t("operations.delete")}</Button>}
 </CardContent></Card>
}
function SnippetEditor({ draft, change, readOnly, busy, clear, save }: { draft: OperationSnippetDraft; change: (value: OperationSnippetDraft) => void; readOnly: boolean; busy: boolean; clear: () => void; save: () => void }) {
 const { t } = useLocale()
 const metadata = (patch: Partial<OperationSnippet>) => change({ ...draft, snippet: { ...draft.snippet, ...patch } })
 return <Card><CardContent className="space-y-4 pt-4"><fieldset disabled={busy} className="space-y-4">
  {readOnly && <p role="status">{t("operations.this_shared_snippet_is_read_only_only_its_author")}</p>}
  <label className="block space-y-2 text-sm">{t("operations.snippet_name")}<Input maxLength={200} readOnly={readOnly} value={draft.snippet.name} onChange={e => metadata({ name: e.target.value })} /></label><label className="block space-y-2 text-sm">{t("operations.description")}<Input maxLength={2000} readOnly={readOnly} value={draft.snippet.description} onChange={e => metadata({ description: e.target.value })} /></label>
  <Select disabled={readOnly} items={[{ value: "sh", label: "POSIX sh" }, { value: "powershell", label: "PowerShell" }]} value={draft.snippet.shell_family} onValueChange={value => { if (value) metadata({ shell_family: value }) }}><SelectTrigger aria-label={t("operations.shell_family")}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="sh">POSIX sh</SelectItem><SelectItem value="powershell">PowerShell</SelectItem></SelectContent></Select>
  <label className="flex items-center gap-2 text-sm"><Checkbox disabled={readOnly} checked={draft.snippet.visibility === "shared"} onCheckedChange={value => metadata({ visibility: value ? "shared" : "private" })} />{t("operations.share_with_administrators")}</label>
  <label className="block space-y-2 text-sm">{t("operations.content")}<Textarea className="min-h-48 font-mono" readOnly={readOnly} spellCheck={false} value={draft.content} onChange={e => change({ ...draft, content: e.target.value })} /></label>
  {!readOnly && !validDraft(draft) && <p role="status" className="text-sm text-muted-foreground">{t("operations.check_name_description_and_content_limits_conten")}</p>}
  <div className="flex justify-end gap-2"><Button variant="outline" onClick={clear}>{t("operations.cancel_and_clear_content")}</Button>{!readOnly && <Button disabled={!validDraft(draft)} onClick={save}>{t("operations.save_version")}</Button>}</div>
 </fieldset></CardContent></Card>
}
