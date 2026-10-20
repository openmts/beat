import { ChevronDownIcon, ChevronRightIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import type { DashboardPanel } from "@/types/metric-workspace"

/** 行标题条：与 Grafana 一致，折叠后只保留标题、面板数量与展开入口。 */
export function WorkspaceRowHeader(props: { row: DashboardPanel; count: number; collapsed: boolean; toggle: () => void }) {
  const { t } = useLocale()
  return <div data-workspace-row data-collapsed={props.collapsed ? "true" : undefined} className="col-span-12 flex flex-wrap items-center gap-2 border-b pb-2">
    <Button variant="ghost" size="icon-sm" aria-expanded={!props.collapsed} aria-label={t("workspace.toggle_row")} onClick={props.toggle}>
      {props.collapsed ? <ChevronRightIcon aria-hidden /> : <ChevronDownIcon aria-hidden />}
    </Button>
    <h3 className="font-medium">{props.row.title}</h3>
    <span className="text-xs text-muted-foreground">{t("workspace.row_panels", { count: props.count })}</span>
  </div>
}
