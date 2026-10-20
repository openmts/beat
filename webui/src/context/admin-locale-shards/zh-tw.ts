import type { Catalog } from "@/context/locales"
import { zhTWAgentEnrollment } from "@/context/zh-tw/agent-enrollment"
import { zhTWAgentRollout } from "@/context/zh-tw/agent-rollout"
import { zhTWAgentSetup } from "@/context/zh-tw/agent-setup"
import { zhTWApplicationRetention } from "@/context/zh-tw/application-retention"
import { zhTWBackup } from "@/context/zh-tw/backup"
import { zhTWDiagnostics } from "@/context/zh-tw/diagnostics"
import { zhTWEnrollmentDiscovery } from "@/context/zh-tw/enrollment-discovery"
import { zhTWEvaluation } from "@/context/zh-tw/evaluation"
import { zhTWIngress } from "@/context/zh-tw/ingress"
import { zhTWLifecycle } from "@/context/zh-tw/lifecycle"
import { zhTWMaintenance } from "@/context/zh-tw/maintenance"
import { zhTWMetricErasure } from "@/context/zh-tw/metric-erasure"
import { zhTWMetricExplore } from "@/context/zh-tw/metric-explore"
import { zhTWMetricWorkspace } from "@/context/zh-tw/metric-workspace"
import { zhTWMetricWorkspaceCollaboration } from "@/context/zh-tw/metric-workspace-collaboration"
import { zhTWMigration } from "@/context/zh-tw/migration"
import { zhTWNotification } from "@/context/zh-tw/notification"
import { zhTWNotificationField } from "@/context/zh-tw/notification-field"
import { zhTWOperation } from "@/context/zh-tw/operation"
import { zhTWSecret } from "@/context/zh-tw/secret"
import { zhTWSecurity } from "@/context/zh-tw/security"
import { zhTWSiteSettings } from "@/context/zh-tw/site-settings"
import { zhTWThemeMarket } from "@/context/zh-tw/theme-market"

// zh-TW 管理端目录分片：只包含管理端专用文案，随语言按需加载（公共外壳与入口分片都不含它）。
export const zhTWAdminCatalog: Catalog = {
  "zh-TW": {
    ...zhTWAgentEnrollment,
    ...zhTWAgentRollout,
    ...zhTWAgentSetup,
    ...zhTWApplicationRetention,
    ...zhTWBackup,
    ...zhTWDiagnostics,
    ...zhTWEnrollmentDiscovery,
    ...zhTWEvaluation,
    ...zhTWIngress,
    ...zhTWLifecycle,
    ...zhTWMaintenance,
    ...zhTWMetricErasure,
    ...zhTWMetricExplore,
    ...zhTWMetricWorkspace,
    ...zhTWMetricWorkspaceCollaboration,
    ...zhTWMigration,
    ...zhTWNotification,
    ...zhTWNotificationField,
    ...zhTWOperation,
    ...zhTWSecret,
    ...zhTWSecurity,
    ...zhTWSiteSettings,
    ...zhTWThemeMarket,
  },
}
