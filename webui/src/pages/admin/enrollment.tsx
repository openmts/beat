import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { enrollmentDiscoveryTranslations } from "@/context/enrollment-discovery-translations"
import { registerLocaleCatalogs, useLocale } from "@/context/locale"
import { useAuth } from "@/context/auth"
import { EnrollmentGrantsPanel } from "./enrollment-grants-panel"
import { EnrollmentRequestsPanel } from "./enrollment-requests-panel"

// 该目录只在这个懒加载页面里使用，因此随页面分片注册，不进入管理端公共分片预算。
registerLocaleCatalogs([enrollmentDiscoveryTranslations])

/** 接入工作台：待审批队列、所有者授权管理与最近接入记录。 */
export default function EnrollmentPage() {
  const { t } = useLocale()
  const { principal } = useAuth()
  const owner = principal?.user.role === "owner"
  return (
    <main className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">{t("discovery.title")}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t("discovery.description")}</p>
      </header>
      <Tabs defaultValue="pending">
        <TabsList>
          <TabsTrigger value="pending">{t("discovery.tab_pending")}</TabsTrigger>
          {owner && <TabsTrigger value="grants">{t("discovery.tab_grants")}</TabsTrigger>}
          <TabsTrigger value="recent">{t("discovery.tab_recent")}</TabsTrigger>
        </TabsList>
        <TabsContent value="pending" className="mt-4">
          <EnrollmentRequestsPanel mode="pending" />
        </TabsContent>
        {owner && (
          <TabsContent value="grants" className="mt-4">
            <EnrollmentGrantsPanel />
          </TabsContent>
        )}
        <TabsContent value="recent" className="mt-4">
          <EnrollmentRequestsPanel mode="recent" />
        </TabsContent>
      </Tabs>
    </main>
  )
}
