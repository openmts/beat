import type { Catalog } from "@/context/locales"
import { idAgentEnrollment } from "@/context/id/agent-enrollment"
import { idAgentRollout } from "@/context/id/agent-rollout"
import { idAgentSetup } from "@/context/id/agent-setup"
import { idApplicationRetention } from "@/context/id/application-retention"
import { idBackup } from "@/context/id/backup"
import { idDiagnostics } from "@/context/id/diagnostics"
import { idEnrollmentDiscovery } from "@/context/id/enrollment-discovery"
import { idEvaluation } from "@/context/id/evaluation"
import { idIngress } from "@/context/id/ingress"
import { idLifecycle } from "@/context/id/lifecycle"
import { idMaintenance } from "@/context/id/maintenance"
import { idMetricErasure } from "@/context/id/metric-erasure"
import { idMetricExplore } from "@/context/id/metric-explore"
import { idMetricWorkspace } from "@/context/id/metric-workspace"
import { idMetricWorkspaceCollaboration } from "@/context/id/metric-workspace-collaboration"
import { idMigration } from "@/context/id/migration"
import { idNotification } from "@/context/id/notification"
import { idNotificationField } from "@/context/id/notification-field"
import { idOperation } from "@/context/id/operation"
import { idSecret } from "@/context/id/secret"
import { idSecurity } from "@/context/id/security"
import { idSiteSettings } from "@/context/id/site-settings"
import { idThemeMarket } from "@/context/id/theme-market"

// id 管理端目录分片：只包含管理端专用文案，随语言按需加载（公共外壳与入口分片都不含它）。
export const idAdminCatalog: Catalog = {
  "id": {
    ...idAgentEnrollment,
    ...idAgentRollout,
    ...idAgentSetup,
    ...idApplicationRetention,
    ...idBackup,
    ...idDiagnostics,
    ...idEnrollmentDiscovery,
    ...idEvaluation,
    ...idIngress,
    ...idLifecycle,
    ...idMaintenance,
    ...idMetricErasure,
    ...idMetricExplore,
    ...idMetricWorkspace,
    ...idMetricWorkspaceCollaboration,
    ...idMigration,
    ...idNotification,
    ...idNotificationField,
    ...idOperation,
    ...idSecret,
    ...idSecurity,
    ...idSiteSettings,
    ...idThemeMarket,
  },
}
