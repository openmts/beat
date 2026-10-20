import { useEffect, useState } from "react"
import { useLocation } from "react-router"
import { useLocale } from "@/context/locale"
import { useAuth } from "@/context/auth"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { PageHeader } from "@/components/page-header"
import AlertRulesPanel from "@/pages/admin/evaluation-rules-panel"
import { EvaluationRecords } from "@/pages/admin/evaluation-records"
import { useEvaluationText } from "@/pages/admin/evaluation-text"
import AlertChannelsPanel from "@/pages/admin/alert-channels-panel"
import AlertEventsPanel from "@/pages/admin/alert-events-panel"
import TrafficReportsPanel from "@/pages/admin/traffic-reports-panel"
import NotificationDeliveriesPanel from "@/pages/admin/notification-deliveries-panel"
import FleetSummariesPanel from "@/pages/admin/fleet-summaries-panel"
import NotificationSettingsPanel from "@/pages/admin/notification-settings-panel"

const alertTabs = ["rules", "evaluations", "evaluation-history", "channels", "events", "summaries", "reports", "deliveries"] as const

/** 深链（例如面板级告警入口）可以落在指定页签；未知页签保持原来的默认值。 */
function initialAlertTab(search: string): string {
  const requested = new URLSearchParams(search).get("tab") ?? ""
  return (alertTabs as readonly string[]).includes(requested) ? requested : "rules"
}

function Alerts() {
  const { t } = useLocale()
  const evaluationText = useEvaluationText()
  const { principal } = useAuth()
  const { search } = useLocation()
  const [activeTab, setActiveTab] = useState(() => initialAlertTab(search))
  // 规则行等深链在 SPA 内跳转时只改 URL，不会重挂载本页，这里跟随地址栏同步页签。
  useEffect(() => { setActiveTab(initialAlertTab(search)) }, [search])
  const [deliveryChannel, setDeliveryChannel] = useState("")

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("alert.manage")} description={t("alert.manage_description")} />
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="rules">{t("alert.rules")}</TabsTrigger>
          <TabsTrigger value="evaluations">{evaluationText("evaluations")}</TabsTrigger>
          <TabsTrigger value="evaluation-history">{evaluationText("history")}</TabsTrigger>
          <TabsTrigger value="channels">{t("alert.channels")}</TabsTrigger>
          <TabsTrigger value="events">{t("alert.events")}</TabsTrigger>
          <TabsTrigger value="summaries">{t("alert.summaries")}</TabsTrigger>
          <TabsTrigger value="reports">{t("traffic_report.title")}</TabsTrigger>
          <TabsTrigger value="deliveries">{t("notification.history")}</TabsTrigger>
          {principal?.user.role === "owner" && <TabsTrigger value="settings">{t("notification.settings")}</TabsTrigger>}
        </TabsList>
        <TabsContent value="rules" className="mt-4">
          <AlertRulesPanel />
        </TabsContent>
        <TabsContent value="evaluations" className="mt-4"><EvaluationRecords mode="states" onIncident={() => setActiveTab("events")} /></TabsContent>
        <TabsContent value="evaluation-history" className="mt-4"><EvaluationRecords mode="history" onIncident={() => setActiveTab("events")} /></TabsContent>
        <TabsContent value="channels" className="mt-4">
          <AlertChannelsPanel onHistory={id => { setDeliveryChannel(id); setActiveTab("deliveries") }} />
        </TabsContent>
        <TabsContent value="events" className="mt-4">
          <AlertEventsPanel />
        </TabsContent>
        <TabsContent value="summaries" className="mt-4"><FleetSummariesPanel onHistory={() => setActiveTab("deliveries")} /></TabsContent>
        <TabsContent value="reports" className="mt-4">
          <TrafficReportsPanel />
        </TabsContent>
        <TabsContent value="deliveries" className="mt-4"><NotificationDeliveriesPanel key={deliveryChannel} initialChannel={deliveryChannel} /></TabsContent>
        <TabsContent value="settings" className="mt-4"><NotificationSettingsPanel /></TabsContent>
      </Tabs>
    </div>
  )
}

export default Alerts
