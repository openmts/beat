import { useState } from "react"
import { useLocale } from "@/context/locale"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { PageHeader } from "@/components/page-header"
import AlertRulesPanel from "@/pages/admin/alert-rules-panel"
import AlertChannelsPanel from "@/pages/admin/alert-channels-panel"
import AlertEventsPanel from "@/pages/admin/alert-events-panel"
import TrafficReportsPanel from "@/pages/admin/traffic-reports-panel"

function Alerts() {
  const { t } = useLocale()
  const [activeTab, setActiveTab] = useState("rules")

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("alert.manage")} description={t("alert.manage_description")} />
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className="max-w-full overflow-x-auto scrollbar-none">
          <TabsTrigger value="rules">{t("alert.rules")}</TabsTrigger>
          <TabsTrigger value="channels">{t("alert.channels")}</TabsTrigger>
          <TabsTrigger value="events">{t("alert.events")}</TabsTrigger>
          <TabsTrigger value="reports">{t("traffic_report.title")}</TabsTrigger>
        </TabsList>
        <TabsContent value="rules" className="mt-4">
          <AlertRulesPanel />
        </TabsContent>
        <TabsContent value="channels" className="mt-4">
          <AlertChannelsPanel />
        </TabsContent>
        <TabsContent value="events" className="mt-4">
          <AlertEventsPanel />
        </TabsContent>
        <TabsContent value="reports" className="mt-4">
          <TrafficReportsPanel />
        </TabsContent>
      </Tabs>
    </div>
  )
}

export default Alerts
