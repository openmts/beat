import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { MetricPolicyEditor } from "./catalog-editor"
import type { MetricCatalog, MetricDefinition } from "@/types/metric-workspace"

export function WorkspaceCatalog(props: { catalog: MetricCatalog; close: () => void; changed: () => void }) {
  const { t } = useLocale()
  const { principal } = useAuth()
  const [search, setSearch] = useState("")
  const [selected, setSelected] = useState<MetricDefinition | null>(null)
  const metrics = props.catalog.metrics.filter((metric) => `${metric.key} ${metric.label}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
  return <>
    <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-4xl">
      <DialogHeader><DialogTitle>{t("workspace.catalog")}</DialogTitle><DialogDescription>{t("workspace.catalog_description")}</DialogDescription></DialogHeader>
      <Input aria-label={t("workspace.search_metrics")} placeholder={t("workspace.search_metrics")} value={search} onChange={(event) => setSearch(event.target.value)} />
      <div className="max-h-[55svh] overflow-auto"><table className="w-full text-left text-sm"><thead className="sticky top-0 bg-background"><tr className="border-b"><th className="p-2">{t("workspace.metric")}</th><th className="p-2">{t("workspace.unit")}</th><th className="p-2">{t("workspace.retention")}</th><th className="p-2">{t("workspace.visibility")}</th><th className="p-2"><span className="sr-only">{t("workspace.edit_policy")}</span></th></tr></thead>
        <tbody>{metrics.map((metric) => <tr key={metric.key} className="border-b last:border-0"><td className="p-2"><div>{metric.label}</div><code className="text-xs text-muted-foreground">{metric.key}</code>{!metric.enabled && <Badge variant="secondary" className="ml-2">{t("workspace.disabled")}</Badge>}</td>
          <td className="p-2 text-xs">{metric.unit.replaceAll("_", " ")}</td><td className="p-2 text-xs">{t("workspace.days", { days: metric.retention_days })}{metric.retention_override_days === null ? ` · ${t("workspace.inherited")}` : ""}</td>
          <td className="p-2 text-xs">{t(metric.public ? "workspace.public_metric" : "workspace.private_metric")}</td><td className="p-2">{principal?.user.role === "owner" && <Button variant="outline" size="sm" onClick={() => setSelected(metric)}>{t("workspace.edit_policy")}</Button>}</td></tr>)}</tbody>
      </table></div>
    </DialogContent></Dialog>
    {selected && <MetricPolicyEditor metric={selected} close={() => setSelected(null)} changed={() => { setSelected(null); props.changed() }} />}
  </>
}
