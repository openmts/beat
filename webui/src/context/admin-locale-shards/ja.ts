import type { Catalog } from "@/context/locales"
import { jaAgentEnrollment } from "@/context/ja/agent-enrollment"
import { jaAgentRollout } from "@/context/ja/agent-rollout"
import { jaAgentSetup } from "@/context/ja/agent-setup"
import { jaApplicationRetention } from "@/context/ja/application-retention"
import { jaBackup } from "@/context/ja/backup"
import { jaDiagnostics } from "@/context/ja/diagnostics"
import { jaEnrollmentDiscovery } from "@/context/ja/enrollment-discovery"
import { jaEvaluation } from "@/context/ja/evaluation"
import { jaIngress } from "@/context/ja/ingress"
import { jaLifecycle } from "@/context/ja/lifecycle"
import { jaMaintenance } from "@/context/ja/maintenance"
import { jaMetricErasure } from "@/context/ja/metric-erasure"
import { jaMetricExplore } from "@/context/ja/metric-explore"
import { jaMetricWorkspace } from "@/context/ja/metric-workspace"
import { jaMetricWorkspaceCollaboration } from "@/context/ja/metric-workspace-collaboration"
import { jaMigration } from "@/context/ja/migration"
import { jaNotification } from "@/context/ja/notification"
import { jaNotificationField } from "@/context/ja/notification-field"
import { jaOperation } from "@/context/ja/operation"
import { jaSecret } from "@/context/ja/secret"
import { jaSecurity } from "@/context/ja/security"
import { jaSiteSettings } from "@/context/ja/site-settings"
import { jaThemeMarket } from "@/context/ja/theme-market"

// ja 管理端目录分片：只包含管理端专用文案，随语言按需加载（公共外壳与入口分片都不含它）。
export const jaAdminCatalog: Catalog = {
  "ja": {
    ...jaAgentEnrollment,
    ...jaAgentRollout,
    ...jaAgentSetup,
    ...jaApplicationRetention,
    ...jaBackup,
    ...jaDiagnostics,
    ...jaEnrollmentDiscovery,
    ...jaEvaluation,
    ...jaIngress,
    ...jaLifecycle,
    ...jaMaintenance,
    ...jaMetricErasure,
    ...jaMetricExplore,
    ...jaMetricWorkspace,
    ...jaMetricWorkspaceCollaboration,
    ...jaMigration,
    ...jaNotification,
    ...jaNotificationField,
    ...jaOperation,
    ...jaSecret,
    ...jaSecurity,
    ...jaSiteSettings,
    ...jaThemeMarket,
  },
}
