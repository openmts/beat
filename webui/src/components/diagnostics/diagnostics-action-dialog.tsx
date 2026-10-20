import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import type { DiagnosticJob } from "@/lib/diagnostics-api"

export type DiagnosticAction = "cancel" | "retry" | "delete"

interface DiagnosticsActionDialogProps {
  job: DiagnosticJob | null
  action: DiagnosticAction
  loading: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}

// 取消、重试和删除共用同一确认外壳，保证三种动作的文案与危险等级一致。
export function DiagnosticsActionDialog(props: DiagnosticsActionDialogProps) {
  const { t } = useLocale()
  return (
    <Dialog open={props.job !== null} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(`diagnostics.${props.action}_confirm_title`)}</DialogTitle>
          <DialogDescription>{t(`diagnostics.${props.action}_confirm_body`)}</DialogDescription>
        </DialogHeader>
        {props.job && (
          <p className="text-sm text-muted-foreground">
            {`${t(`diagnostics.kind.${props.job.kind}`)} · ${props.job.source_node_name || props.job.source_node_id} → ${props.job.target_name || props.job.target_id}`}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button variant={props.action === "delete" ? "destructive" : "default"} disabled={props.loading} onClick={props.onConfirm}>
            {t(`diagnostics.${props.action}`)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
