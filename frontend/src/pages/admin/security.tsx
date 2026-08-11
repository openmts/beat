import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { PageHeader } from "@/components/page-header"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { SecurityAccountPanel } from "@/pages/admin/security-account-panel"
import { SecurityAdminsPanel } from "@/pages/admin/security-admins-panel"
import { SecurityAuditPanel } from "@/pages/admin/security-audit-panel"
import { SecurityBackupPanel } from "@/pages/admin/security-backup-panel"
import { SecuritySessionsPanel } from "@/pages/admin/security-sessions-panel"
import { SecurityTOTPPanel } from "@/pages/admin/security-totp-panel"

export default function Security() {
  const { principal } = useAuth()
  const { t } = useLocale()
  const owner = principal?.user.role === "owner"

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title={t("security.title")} description={t("security.description")} />
      <Tabs defaultValue="account">
        <TabsList className="max-w-full overflow-x-auto scrollbar-none">
          <TabsTrigger value="account">{t("security.account")}</TabsTrigger>
          <TabsTrigger value="totp">{t("security.two_factor")}</TabsTrigger>
          <TabsTrigger value="sessions">{t("security.sessions")}</TabsTrigger>
          {owner && <TabsTrigger value="administrators">{t("security.administrators")}</TabsTrigger>}
          {owner && <TabsTrigger value="backup">{t("backup.title")}</TabsTrigger>}
          <TabsTrigger value="audit">{t("security.audit")}</TabsTrigger>
        </TabsList>
        <TabsContent value="account"><SecurityAccountPanel /></TabsContent>
        <TabsContent value="totp"><SecurityTOTPPanel /></TabsContent>
        <TabsContent value="sessions"><SecuritySessionsPanel /></TabsContent>
        {owner && <TabsContent value="administrators"><SecurityAdminsPanel /></TabsContent>}
        {owner && <TabsContent value="backup"><SecurityBackupPanel /></TabsContent>}
        <TabsContent value="audit"><SecurityAuditPanel /></TabsContent>
      </Tabs>
    </div>
  )
}
