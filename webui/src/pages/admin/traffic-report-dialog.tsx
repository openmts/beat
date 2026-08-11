import type { AlertChannel, ManagedNode } from "@/types"
import type { TrafficReportForm } from "@/pages/admin/traffic-report-config"
import { TrafficReportFields } from "@/pages/admin/traffic-report-fields"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

interface TrafficReportDialogProps {
  open: boolean
  editing: boolean
  loading: boolean
  error: string | null
  form: TrafficReportForm
  nodes: ManagedNode[]
  channels: AlertChannel[]
  onOpenChange: (open: boolean) => void
  onChange: (form: TrafficReportForm) => void
  onSave: () => void
}

export function TrafficReportDialog(props: TrafficReportDialogProps) {
  const { t } = useLocale()
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t(props.editing ? "traffic_report.edit" : "traffic_report.create")}</DialogTitle>
          <DialogDescription>{t("traffic_report.description")}</DialogDescription>
        </DialogHeader>
        {props.error ? <Alert variant="destructive"><AlertDescription>{props.error}</AlertDescription></Alert> : null}
        <TrafficReportFields form={props.form} nodes={props.nodes} channels={props.channels} t={t} onChange={props.onChange} />
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button onClick={props.onSave} disabled={props.loading}>{t("app.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
