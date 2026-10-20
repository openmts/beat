import { registerLocaleCatalogs } from "@/context/locale"
import { adminShellTranslations } from "@/context/admin-shell-translations"
import { agentEnrollmentTranslations } from "@/context/agent-enrollment-translations"
import { agentRolloutTranslations } from "@/context/agent-rollout-translations"
import { agentSetupTranslations } from "@/context/agent-setup-translations"
import { applicationRetentionTranslations } from "@/context/application-retention-translations"
import { backupTranslations } from "@/context/backup-translations"
import { diagnosticsTranslations } from "@/context/diagnostics-translations"
import { evaluationTranslations } from "@/context/evaluation-translations"
import { ingressTranslations } from "@/context/ingress-translations"
import { lifecycleTranslations } from "@/context/lifecycle-translations"
import { maintenanceTranslations } from "@/context/maintenance-translations"
import { migrationTranslations } from "@/context/migration-translations"
import { metricErasureTranslations } from "@/context/metric-erasure-translations"
import { notificationFieldTranslations } from "@/context/notification-field-translations"
import { notificationTranslations } from "@/context/notification-translations"
import { operationTranslations } from "@/context/operation-translations"
import { secretTranslations } from "@/context/secret-translations"
import { securityTranslations } from "@/context/security-translations"
import { siteSettingsTranslations } from "@/context/site-settings-translations"
import { themeMarketTranslations } from "@/context/theme-market-translations"

// admin-catalogs 只在管理端分片被 import，公共外壳不会加载这些文案目录。
registerLocaleCatalogs([
  adminShellTranslations,
  agentEnrollmentTranslations,
  agentRolloutTranslations,
  agentSetupTranslations,
  applicationRetentionTranslations,
  backupTranslations,
  diagnosticsTranslations,
  evaluationTranslations,
  ingressTranslations,
  lifecycleTranslations,
  maintenanceTranslations,
  migrationTranslations,
  metricErasureTranslations,
  notificationFieldTranslations,
  notificationTranslations,
  operationTranslations,
  secretTranslations,
  securityTranslations,
  siteSettingsTranslations,
  themeMarketTranslations,
])
