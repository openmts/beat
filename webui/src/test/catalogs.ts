import { translateFrom, type Catalog, type Locale } from "@/context/locales"
import { acceleratorTranslations } from "@/context/accelerator-translations"
import { agentEnrollmentTranslations } from "@/context/agent-enrollment-translations"
import { agentRolloutTranslations } from "@/context/agent-rollout-translations"
import { agentSetupTranslations } from "@/context/agent-setup-translations"
import { applicationRetentionTranslations } from "@/context/application-retention-translations"
import { backupTranslations } from "@/context/backup-translations"
import { baseTranslations } from "@/context/base-translations"
import { diagnosticsTranslations } from "@/context/diagnostics-translations"
import { enrollmentDiscoveryTranslations } from "@/context/enrollment-discovery-translations"
import { evaluationTranslations } from "@/context/evaluation-translations"
import { fleetTranslations } from "@/context/fleet-translations"
import { ingressTranslations } from "@/context/ingress-translations"
import { lifecycleTranslations } from "@/context/lifecycle-translations"
import { maintenanceTranslations } from "@/context/maintenance-translations"
import { migrationTranslations } from "@/context/migration-translations"
import { metricErasureTranslations } from "@/context/metric-erasure-translations"
import { metricExploreTranslations } from "@/context/metric-explore-translations"
import { metricWorkspaceTranslations } from "@/context/metric-workspace-translations"
import { monitorTranslations } from "@/context/monitor-translations"
import { notificationFieldTranslations } from "@/context/notification-field-translations"
import { notificationTranslations } from "@/context/notification-translations"
import { operationTranslations } from "@/context/operation-translations"
import { secretTranslations } from "@/context/secret-translations"
import { securityTranslations } from "@/context/security-translations"
import { siteSettingsTranslations } from "@/context/site-settings-translations"
import { telemetryTranslations } from "@/context/telemetry-translations"
import { themeMarketTranslations } from "@/context/theme-market-translations"
import { themeTranslations } from "@/context/theme-translations"
import { trafficReportTranslations } from "@/context/traffic-report-translations"
import { zhCNPublicCatalog } from "@/context/public-locales/zh-cn"
import { zhTWPublicCatalog } from "@/context/public-locales/zh-tw"
import { jaPublicCatalog } from "@/context/public-locales/ja"
import { idPublicCatalog } from "@/context/public-locales/id"
import { zhCNAdminCatalog } from "@/context/admin-locale-shards/zh-cn"
import { zhTWAdminCatalog } from "@/context/admin-locale-shards/zh-tw"
import { jaAdminCatalog } from "@/context/admin-locale-shards/ja"
import { idAdminCatalog } from "@/context/admin-locale-shards/id"
import { publicWorkspaceEn } from "@/context/public-workspace/en"
import { publicWorkspaceZhCN } from "@/context/public-workspace/zh-cn"
import { publicWorkspaceZhTW } from "@/context/public-workspace/zh-tw"
import { publicWorkspaceJa } from "@/context/public-workspace/ja"
import { publicWorkspaceId } from "@/context/public-workspace/id"

/**
 * 全部目录（含管理端与功能内目录）。生产代码按分片注册，测试替身需要一次看全，
 * 目录完整性门禁也以这份清单为准。
 */
const productCatalogs: Catalog[] = [
  baseTranslations, monitorTranslations, trafficReportTranslations, themeTranslations, telemetryTranslations,
  fleetTranslations, agentEnrollmentTranslations, agentRolloutTranslations, agentSetupTranslations, applicationRetentionTranslations,
  backupTranslations, diagnosticsTranslations, maintenanceTranslations, metricErasureTranslations,
  metricWorkspaceTranslations, notificationFieldTranslations, notificationTranslations, operationTranslations,
  secretTranslations, securityTranslations, siteSettingsTranslations, themeMarketTranslations,
  migrationTranslations, ingressTranslations, lifecycleTranslations,
  acceleratorTranslations, evaluationTranslations, enrollmentDiscoveryTranslations, metricExploreTranslations,
  publicWorkspaceEn, publicWorkspaceZhCN, publicWorkspaceZhTW, publicWorkspaceJa, publicWorkspaceId,
]


/**
 * 参与门禁的全部目录。公共外壳的非英文语言放在按语言拆分的分片里（成为独立 chunk），
 * 测试把它们和产品目录一起登记，才能校验已发布语言的键集完整性。
 */
export const allCatalogs: Catalog[] = [
  ...productCatalogs, zhCNPublicCatalog, zhTWPublicCatalog, jaPublicCatalog, idPublicCatalog,
  zhCNAdminCatalog, zhTWAdminCatalog, jaAdminCatalog, idAdminCatalog,
]

/** 测试里替代 useLocale：返回真实目录解析出的文案，断言仍针对实际文案而不是键名。 */
export function testLocaleValue(locale: Locale = "en") {
  return {
    locale,
    setLocale: () => undefined,
    t: (key: string, params?: Record<string, string | number>): string =>
      translateFrom(allCatalogs, locale, key, params),
  }
}
