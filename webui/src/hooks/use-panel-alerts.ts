import { useEffect, useState } from "react"
import { listPanelAlerts, type DashboardPanelAlert } from "@/lib/metric-collaboration-api"

/** v25 面板级告警关联：按看板加载，写入后局部替换该面板的关联。 */
export function usePanelAlerts(dashboardId: string) {
  const [alerts, setAlerts] = useState<DashboardPanelAlert[]>([])
  useEffect(() => {
    if (!dashboardId || dashboardId === "new") { setAlerts([]); return }
    const controller = new AbortController()
    listPanelAlerts(dashboardId, controller.signal).then(setAlerts).catch(() => { if (!controller.signal.aborted) setAlerts([]) })
    return () => controller.abort()
  }, [dashboardId])
  return {
    alerts,
    apply: (panelId: string, values: DashboardPanelAlert[]) =>
      setAlerts((previous) => [...previous.filter((value) => value.panel_id !== panelId), ...values]),
    forPanel: (panelId: string) => alerts.filter((value) => value.panel_id === panelId),
  }
}

export type PanelAlertCollection = ReturnType<typeof usePanelAlerts>
