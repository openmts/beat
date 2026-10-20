import { useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useLocale } from "@/context/locale"
import { InstalledThemes } from "@/pages/admin/theme-installed-panel"
import { MarketSources } from "@/pages/admin/theme-market-panel"

export function ThemesPanel() {
  const { t } = useLocale()
  const [revision, setRevision] = useState(0)
  return <Tabs defaultValue="installed">
    <TabsList>
      <TabsTrigger value="installed">{t("theme.installed")}</TabsTrigger>
      <TabsTrigger value="market">{t("theme.market")}</TabsTrigger>
    </TabsList>
    <TabsContent value="installed"><InstalledThemes revision={revision} /></TabsContent>
    <TabsContent value="market"><MarketSources onInstalled={() => setRevision((value) => value + 1)} /></TabsContent>
  </Tabs>
}
