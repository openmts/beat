export type TimeRange = "1h" | "6h" | "24h" | "7d"

export const RANGE_HOURS: Record<TimeRange, number> = {
  "1h": 1,
  "6h": 6,
  "24h": 24,
  "7d": 168,
}

export const CUSTOM_METRIC_COLORS = [
  "#7c3aed",
  "#e11d48",
  "#0891b2",
  "#ca8a04",
  "#dc2626",
  "#9333ea",
  "#0284c7",
  "#16a34a",
]

export const CHART_CONFIGS = [
  { key: "cpu", titleKey: "metrics.cpu", color: "#2563eb", valueFormat: "percent" },
  { key: "memory", titleKey: "metrics.memory", color: "#059669", valueFormat: "percent" },
  { key: "disk", titleKey: "metrics.disk", color: "#d97706", valueFormat: "percent" },
  { key: "swap", titleKey: "metrics.swap", color: "#7c3aed", valueFormat: "percent" },
  { key: "load1", titleKey: "metrics.load", color: "#e11d48", valueFormat: "number" },
  { key: "processes", titleKey: "metrics.processes", color: "#0891b2", valueFormat: "number" },
  { key: "tcp_connections", titleKey: "metrics.connections", color: "#4f46e5", valueFormat: "number" },
  { key: "disk_read", titleKey: "metrics.disk_read", color: "#ca8a04", valueFormat: "bytes-per-second" },
  { key: "disk_write", titleKey: "metrics.disk_write", color: "#dc2626", valueFormat: "bytes-per-second" },
  { key: "net_recv", titleKey: "metrics.net_recv", color: "#9333ea", valueFormat: "bytes-per-second" },
  { key: "net_sent", titleKey: "metrics.net_sent", color: "#0284c7", valueFormat: "bytes-per-second" },
] as const
