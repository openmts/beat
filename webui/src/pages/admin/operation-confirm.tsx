import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import type { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
type Flow = ReturnType<typeof useConfirmedOperation>
export function OperationConfirm({ flow }: { flow: Flow }) { return flow.action ? <Confirmation key={flow.action.title} flow={flow} /> : null }
function Confirmation({ flow }: { flow: Flow }) {
 const { t } = useLocale()
 const [password, setPassword] = useState(""), [code, setCode] = useState(""), [accepted, setAccepted] = useState(false)
 const submit = async (event: FormEvent) => { event.preventDefault(); await flow.confirm(password, code); setPassword(""); setCode("") }
 return <Dialog open onOpenChange={open => { if (!open) flow.close() }}><DialogContent><form onSubmit={submit} className="space-y-4">
  <DialogHeader><DialogTitle>{flow.action?.title}</DialogTitle><DialogDescription>{flow.action?.description}</DialogDescription></DialogHeader>
  {flow.needsProof && <><Label htmlFor="operation-password">{t("operations.current_password")}</Label><Input id="operation-password" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} disabled={flow.busy} />
   {flow.totp && <><Label htmlFor="operation-code">{t("operations.authenticator_code")}</Label><Input id="operation-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={e => setCode(e.target.value)} disabled={flow.busy} /></>}</>}
  {flow.action?.uncertain && <label className="flex items-start gap-2 text-sm"><Checkbox checked={accepted} onCheckedChange={v => setAccepted(Boolean(v))} />{t("operations.i_understand_the_original_operation_may_have_tak")}</label>}
  {flow.error && <p role="alert" className="text-sm text-destructive">{flow.error}</p>}
  <DialogFooter><Button type="button" variant="outline" disabled={flow.busy} onClick={flow.close}>{t("operations.cancel")}</Button><Button type="submit" disabled={flow.busy || Boolean(flow.action?.uncertain && !accepted) || (flow.needsProof && (!password || (flow.totp && !/^\d{6}$/.test(code))))}>{flow.busy ? (t("operations.working")) : (t("operations.confirm"))}</Button></DialogFooter>
 </form></DialogContent></Dialog>
}
