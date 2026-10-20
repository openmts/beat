import type { Catalog } from "@/context/locales"
import { zhCNAgentEnrollment } from "@/context/zh-cn/agent-enrollment"
import { zhCNAgentRollout } from "@/context/zh-cn/agent-rollout"
import { zhCNAgentSetup } from "@/context/zh-cn/agent-setup"
import { zhCNApplicationRetention } from "@/context/zh-cn/application-retention"
import { zhCNBackup } from "@/context/zh-cn/backup"
import { zhCNDiagnostics } from "@/context/zh-cn/diagnostics"
import { zhCNEnrollmentDiscovery } from "@/context/zh-cn/enrollment-discovery"
import { zhCNEvaluation } from "@/context/zh-cn/evaluation"
import { zhCNIngress } from "@/context/zh-cn/ingress"
import { zhCNLifecycle } from "@/context/zh-cn/lifecycle"
import { zhCNMaintenance } from "@/context/zh-cn/maintenance"
import { zhCNMetricErasure } from "@/context/zh-cn/metric-erasure"
import { zhCNMetricExplore } from "@/context/zh-cn/metric-explore"
import { zhCNMetricWorkspace } from "@/context/zh-cn/metric-workspace"
import { zhCNMetricWorkspaceCollaboration } from "@/context/zh-cn/metric-workspace-collaboration"
import { zhCNMigration } from "@/context/zh-cn/migration"
import { zhCNNotification } from "@/context/zh-cn/notification"
import { zhCNNotificationField } from "@/context/zh-cn/notification-field"
import { zhCNOperation } from "@/context/zh-cn/operation"
import { zhCNSecret } from "@/context/zh-cn/secret"
import { zhCNSecurity } from "@/context/zh-cn/security"
import { zhCNSiteSettings } from "@/context/zh-cn/site-settings"
import { zhCNThemeMarket } from "@/context/zh-cn/theme-market"

// zh-CN 管理端目录分片：只包含管理端专用文案，随语言按需加载（公共外壳与入口分片都不含它）。
export const zhCNAdminCatalog: Catalog = {
  "zh-CN": {
    ...zhCNAgentEnrollment,
    ...zhCNAgentRollout,
    ...zhCNAgentSetup,
    ...zhCNApplicationRetention,
    ...zhCNBackup,
    ...zhCNDiagnostics,
    ...zhCNEnrollmentDiscovery,
    ...zhCNEvaluation,
    ...zhCNIngress,
    ...zhCNLifecycle,
    ...zhCNMaintenance,
    ...zhCNMetricErasure,
    ...zhCNMetricExplore,
    ...zhCNMetricWorkspace,
    ...zhCNMetricWorkspaceCollaboration,
    ...zhCNMigration,
    ...zhCNNotification,
    ...zhCNNotificationField,
    ...zhCNOperation,
    ...zhCNSecret,
    ...zhCNSecurity,
    ...zhCNSiteSettings,
    ...zhCNThemeMarket,
  },
}
