import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { parsePastedPanelJSON } from "@/lib/workspace-paste-panel"
import type { DashboardDefinition, DashboardPanel, MetricDefinition } from "@/types/metric-workspace"

/** 粘贴面板 JSON：解析与校验都在本地完成，取消或报错都不产生任何变更。 */
export function WorkspacePastePanel(props: { definition: DashboardDefinition; metrics: MetricDefinition[]; close: () => void; append: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const [text, setText] = useState("")
  const [error, setError] = useState<string | null>(null)
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent className="sm:max-w-2xl">
    <DialogHeader><DialogTitle>{t("workspace.paste_panel")}</DialogTitle>
      <DialogDescription>{t("workspace.paste_panel_description")}</DialogDescription></DialogHeader>
    <div className="grid gap-2"><Label htmlFor="workspace-paste-panel">{t("workspace.paste_panel_label")}</Label>
      <Textarea id="workspace-paste-panel" rows={10} spellCheck={false} value={text} placeholder={t("workspace.paste_panel_placeholder")}
        onChange={(event) => { setText(event.target.value); setError(null) }} /></div>
    {error && <p role="alert" className="text-sm text-destructive">{t(error)}</p>}
    <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button>
      <Button disabled={!text.trim()} onClick={() => {
        const result = parsePastedPanelJSON(text, props.definition, props.metrics)
        if ("error" in result) { setError(result.error); return }
        props.append(result.panel); props.close()
      }}>{t("workspace.add_panel")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
