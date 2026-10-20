import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLocale } from "@/context/locale"

export function WorkspaceShare(props: { link: string; close: () => void }) {
  const { t } = useLocale()
  const [status, setStatus] = useState<"copied" | "failed" | null>(null)
  async function copy() {
    try { await navigator.clipboard.writeText(props.link); setStatus("copied") }
    catch { setStatus("failed") }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent><DialogHeader>
    <DialogTitle>{t("workspace.share")}</DialogTitle><DialogDescription>{t("workspace.share_description")}</DialogDescription></DialogHeader>
    <div className="grid gap-2"><Label htmlFor="workspace-share-link">{t("workspace.share_link")}</Label>
      <Input id="workspace-share-link" readOnly value={props.link} onFocus={(event) => event.currentTarget.select()} /></div>
    {status && <p role="status" className={status === "failed" ? "text-sm text-destructive" : "text-sm text-muted-foreground"}>{t(status === "copied" ? "workspace.copied" : "workspace.copy_failed")}</p>}
    <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.close")}</Button>
      <Button onClick={() => { void copy() }}>{t("workspace.copy_link")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
