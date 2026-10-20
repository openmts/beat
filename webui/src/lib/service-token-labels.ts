import type { ServiceResourceKind, ServiceScope } from "@/types/service-tokens"

/** 服务令牌范围与资源类型只在这里映射到 security.* 目录键，未知取值原样显示标识。 */
const scopeLabelKeys: Record<ServiceScope, string> = {
  "nodes:read": "security.token_scope_nodes_read",
  "nodes:write-presentation": "security.token_scope_nodes_write_presentation",
  "metrics:read": "security.token_scope_metrics_read",
  "network-tasks:read": "security.token_scope_network_tasks_read",
  "network-tasks:run-test": "security.token_scope_network_tasks_run_test",
  "alerts:read": "security.token_scope_alerts_read",
  "notification-test:send": "security.token_scope_notification_test_send",
  "traffic-reports:read": "security.token_scope_traffic_reports_read",
  "traffic-reports:run-test": "security.token_scope_traffic_reports_run_test",
  "backups:create": "security.token_scope_backups_create",
  "backups:read-metadata": "security.token_scope_backups_read_metadata",
  "backups:download": "security.token_scope_backups_download",
  "audit:read": "security.token_scope_audit_read",
  "health:read": "security.token_scope_health_read",
}

const resourceLabelKeys: Record<ServiceResourceKind, string> = {
  node: "security.token_resource_node",
  group: "security.token_resource_group",
  channel: "security.token_resource_channel",
}

function catalogLabel(t: (key: string) => string, key: string | undefined, fallback: string): string {
  if (!key) return fallback
  const value = t(key)
  return value === key ? fallback : value
}

export function serviceScopeLabel(scope: ServiceScope, t: (key: string) => string): string {
  return catalogLabel(t, scopeLabelKeys[scope], scope)
}

export function serviceResourceLabel(kind: ServiceResourceKind, t: (key: string) => string): string {
  return catalogLabel(t, resourceLabelKeys[kind], kind)
}
