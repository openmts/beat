import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLocale } from "@/context/locale"
import { duplicateDashboardName, maxDashboardTitle } from "@/lib/workspace-duplicate"

export function WorkspaceDuplicate(props: { title: string; close: () => void; confirm: (title: string) => void }) {
  const { t } = useLocale()
  const [value, setValue] = useState(() => duplicateDashboardName(props.title, t("workspace.duplicate_suffix")))
  const name = value.trim()
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent><DialogHeader>
    <DialogTitle>{t("workspace.duplicate")}</DialogTitle><DialogDescription>{t("workspace.duplicate_description")}</DialogDescription>
  </DialogHeader>
  <div className="grid gap-2"><Label htmlFor="workspace-duplicate-name">{t("workspace.duplicate_name")}</Label>
    <Input id="workspace-duplicate-name" value={value} maxLength={maxDashboardTitle} onChange={(event) => setValue(event.target.value)} /></div>
  <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button>
    <Button disabled={!name} onClick={() => { props.confirm(name); props.close() }}>{t("workspace.confirm")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
