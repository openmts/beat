import { DatabaseIcon } from "lucide-react"

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useLocale } from "@/context/locale"
import ApplicationRetentionPanel from "@/pages/admin/application-retention-panel"
import MaintenancePanel from "@/pages/admin/maintenance-panel"
import { MetricErasurePanel } from "@/pages/admin/metric-erasure-panel"

/** MTS 保留策略是常规自动流程，应用历史有独立开关，擦除历史是例外且不可逆，所以三者分页签。 */
export function DataStorageSection() {
  const { t } = useLocale()
  return (
    <section className="border-t pt-7">
      <div className="mb-5 flex items-start gap-3">
        <DatabaseIcon className="mt-0.5 size-5 text-muted-foreground" />
        <div>
          <h2 className="text-base font-semibold">{t("storage.title")}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{t("storage.description")}</p>
        </div>
      </div>
      <Tabs defaultValue="retention">
        <TabsList>
          <TabsTrigger value="retention">{t("storage.tab_retention")}</TabsTrigger>
          <TabsTrigger value="application">{t("storage.tab_application")}</TabsTrigger>
          <TabsTrigger value="erase">{t("storage.tab_erase")}</TabsTrigger>
        </TabsList>
        <TabsContent value="retention" className="pt-5"><MaintenancePanel /></TabsContent>
        <TabsContent value="application" className="pt-5"><ApplicationRetentionPanel /></TabsContent>
        <TabsContent value="erase" className="pt-5"><MetricErasurePanel /></TabsContent>
      </Tabs>
    </section>
  )
}

export default DataStorageSection
