import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLocale } from "@/context/locale"
import { reauthenticateAdmin } from "@/lib/security-api"

export interface WorkspaceConfirmation { kind: "discard" | "delete" | "rollback"; action: () => void }

export function WorkspaceConfirm(props: { confirmation: WorkspaceConfirmation; close: () => void }) {
  const { t } = useLocale()
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent><DialogHeader>
    <DialogTitle>{t(`workspace.confirm_${props.confirmation.kind}`)}</DialogTitle><DialogDescription>{t(`workspace.${props.confirmation.kind}_description`)}</DialogDescription>
  </DialogHeader><DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button>
    <Button onClick={() => { props.confirmation.action(); props.close() }}>{t("workspace.confirm")}</Button></DialogFooter></DialogContent></Dialog>
}

export function WorkspaceReauthenticate(props: { action: () => Promise<void>; close: () => void; confirmed: (until: string | null) => void; totp: boolean }) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  async function submit() {
    setBusy(true); setError(null)
    try {
      const session = await reauthenticateAdmin(password, code)
      setPassword(""); setCode("")
      props.confirmed(session.reauthenticated_until)
      await props.action()
      props.close()
    } catch (error) { setError(error instanceof Error ? error.message : "workspace.save_failed") }
    finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) props.close() }}><DialogContent><DialogHeader>
    <DialogTitle>{t("workspace.confirm_publish")}</DialogTitle><DialogDescription>{t("workspace.publish_description")}</DialogDescription></DialogHeader>
    <div className="grid gap-2"><Label htmlFor="workspace-password">{t("workspace.password")}</Label><Input id="workspace-password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} /></div>
    {props.totp && <div className="grid gap-2"><Label htmlFor="workspace-totp">{t("workspace.totp_code")}</Label><Input id="workspace-totp" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} /></div>}
    {error && <p role="alert" className="text-sm text-destructive">{t(error)}</p>}
    <DialogFooter><Button disabled={busy} variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button><Button disabled={busy || !password} onClick={() => { void submit() }}>{t("workspace.reauthenticate")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
