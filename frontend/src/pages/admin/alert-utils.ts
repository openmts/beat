export const severityColors: Record<string, "default" | "destructive" | "secondary"> = {
  critical: "destructive",
  warning: "default",
  info: "secondary",
}

export const availabilityMetric = "heartbeat_age_seconds"

export function buildAlertMetricOptions(t: (key: string) => string) {
  return [
    { label: t("alert.metric.cpu"), value: "cpu" },
    { label: t("alert.metric.memory"), value: "memory" },
    { label: t("alert.metric.disk"), value: "disk" },
    { label: t("alert.metric.swap"), value: "swap" },
    { label: t("alert.metric.load"), value: "load1" },
    { label: t("alert.metric.processes"), value: "processes" },
    { label: t("alert.metric.tcp"), value: "tcp_connections" },
    { label: t("alert.metric.udp"), value: "udp_connections" },
    { label: t("alert.metric.net_recv"), value: "net_recv" },
    { label: t("alert.metric.net_sent"), value: "net_sent" },
    { label: t("alert.metric.traffic_usage"), value: "traffic_usage_percent" },
    { label: t("alert.metric.availability"), value: availabilityMetric },
  ]
}

export function isAvailabilityMetric(metric: string) {
  return metric === availabilityMetric
}

export function availabilityRuleDefaults() {
  return { operator: "gt", threshold: 90, duration: 30 }
}

export function formatAlertCondition(
  rule: { metric: string; operator: string; threshold: number; duration: number },
  t: (key: string) => string,
) {
  if (isAvailabilityMetric(rule.metric)) {
    return `${rule.threshold}s ${t("alert.offline_after_short")} · ${rule.duration}s ${t("alert.debounce_short")}`
  }
  return `${rule.operator} ${rule.threshold} (${rule.duration}s)`
}

export function buildAlertOperatorOptions(greaterThan: string, lessThan: string) {
  return [
    { label: `> (${greaterThan})`, value: "gt" },
    { label: `< (${lessThan})`, value: "lt" },
  ]
}

export function buildAlertSeverityOptions(critical: string, warning: string, info: string) {
  return [
    { label: critical, value: "critical" },
    { label: warning, value: "warning" },
    { label: info, value: "info" },
  ]
}

export function messageFromError(err: unknown) {
  return err instanceof Error ? err.message : "The request failed"
}

export function isValidWebhookURL(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === "http:" || url.protocol === "https:"
  } catch {
    return false
  }
}
