import type { Node } from "@/types"
import type { WorkspaceSources } from "@/lib/metric-workspace-api"
import type { MetricDashboard, MetricDefinition, MetricQuery, MetricQueryResult } from "@/types/metric-workspace"

export function workspaceNode(id = "node-one"): Node {
  return { id, name: "Production host", alias: "", group_id: "group-one", host: "192.0.2.1", port: 22,
    status: "online", ssh_public_key: "", cpu_model: "CPU", os: "linux", platform: "linux", os_version: "1",
    kernel: "6", arch: "amd64", virtualization: "", agent_version: "test", sort_order: 0, tags: [], is_public: true,
    public_remark: "", last_seen: "2026-09-13T01:00:00Z", created_at: "2026-09-13T00:00:00Z", updated_at: "2026-09-13T01:00:00Z" }
}

export function workspaceMetric(key = "cpu", unit = "percent"): MetricDefinition {
  return { key, unit, label: key === "cpu" ? "CPU usage" : "Memory usage", description: "", kind: "gauge", source: "builtin",
    aggregations: ["avg", "min", "max", "last"], default_aggregation: "avg", enabled: true, public: true, retention_days: 30, retention_override_days: null, revision: 1 }
}

export function workspaceDashboard(): MetricDashboard {
  return { id: "admin-default", scope: "admin", revision: 0, has_previous: false, created_at: "", updated_at: "", definition: {
    version: 1, title: "Node overview", description: "", time_range_seconds: 3600, refresh_seconds: 0,
    variables: [{ name: "node", type: "node", label: "Node" }], panels: [{ id: "cpu", title: "CPU usage",
      type: "timeseries", size: "medium", legend: true, thresholds: [], series: [{ ref_id: "A", node_id: "$node",
        metric: "cpu", aggregation: "avg", label: "CPU", color: "blue" }] }],
  } }
}

export function workspaceSources(): WorkspaceSources {
  return { catalog: { version: 1, revision: 1, metrics: [workspaceMetric(), workspaceMetric("memory")] },
    dashboards: [workspaceDashboard()], nodes: [workspaceNode()],
    groups: [{ id: "group-one", name: "Production", sort_order: 0, is_default: true, created_at: "", updated_at: "" }] }
}

export function workspaceQuery(): MetricQuery {
  return { from: "2026-09-13T00:00:00.000Z", to: "2026-09-13T01:00:00.000Z", max_points: 600,
    series: [{ ref_id: "A", node_id: "node-one", metric: "cpu", aggregation: "avg" }] }
}

export function workspaceResult(): MetricQueryResult {
  return { from: workspaceQuery().from, to: workspaceQuery().to, interval_ms: 6000, series: [{ ...workspaceQuery().series[0],
    label: "CPU usage", unit: "percent", available_from: "2026-09-13T00:00:00.000Z", state: "ok", points: [
      { timestamp: Date.parse(workspaceQuery().from), value: 40 },
      { timestamp: Date.parse(workspaceQuery().from) + 6000, value: null },
    ],
  }] }
}
