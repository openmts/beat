import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { getOperationTransfers } from "@/lib/operation-transfers"
import { OperationStatus } from "./operation-status"
import { OperationTransferCreate } from "./operation-transfer-create"
import { OperationTransferDetail } from "./operation-transfer-detail"
export function OperationTransfers({ task }: { task: (id: string) => void }) {
 const { locale, t } = useLocale()
 const resource = useOperationResource(getOperationTransfers, 5000)
 const [creating, setCreating] = useState(false), [selected, setSelected] = useState<{ id: string; file?: File } | null>(null)
 return <section className="space-y-4"><div className="flex items-center justify-between"><h2 className="font-semibold">{t("operations.file_transfers")}</h2><Button onClick={() => setCreating(true)}>{t("operations.new_transfer")}</Button></div>
  {resource.error && <p role="alert" className="text-destructive">{resource.error}</p>}
  {resource.data?.map(value => <Card key={value.id}><CardContent className="flex flex-wrap items-center gap-3 pt-4"><div className="flex-1"><p>{value.direction === "deploy" ? (t("operations.file_deployment")) : (t("operations.file_collection"))}</p><p className="text-xs text-muted-foreground">{(value.size / 1048576).toFixed(2)} MiB · {t("operations.expires")} {formatDateTime(locale, value.expires_at)}</p></div><OperationStatus state={value.state} /><Button variant="outline" onClick={() => setSelected({ id: value.id })}>{t("operations.details")}</Button></CardContent></Card>)}
  {!resource.loading && !resource.data?.length && <p className="py-8 text-center text-sm text-muted-foreground">{t("operations.no_file_transfers")}</p>}
  {selected && <OperationTransferDetail key={selected.id} id={selected.id} initialFile={selected.file} close={() => setSelected(null)} task={task} />}
  {creating && <OperationTransferCreate close={() => setCreating(false)} created={(id, file) => { setCreating(false); setSelected({ id, file }); resource.reload() }} />}
 </section>
}
