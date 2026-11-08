import { PageHeader } from "@/components/page-header"
import { useLocale } from "@/context/locale"
import { ThemesPanel } from "@/pages/admin/theme-panels"

function Themes() {
  const { t } = useLocale()
  return <div className="mx-auto w-full max-w-6xl space-y-6">
    <PageHeader title={t("theme.title")} description={t("theme.description")} />
    <ThemesPanel />
  </div>
}

export default Themes
