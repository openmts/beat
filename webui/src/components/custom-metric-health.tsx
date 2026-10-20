import { useLocale } from "@/context/locale"
import { Badge } from "@/components/ui/badge"
import type { CustomMetricHealth, CustomMetricHealthNode, CustomMetricHealthState, Node } from "@/types"

const stateVariants: Record<CustomMetricHealthState, "default" | "secondary" | "destructive" | "outline"> = {
  ok: "default",
  stale: "secondary",
  missing: "destructive",
  unverified: "destructive",
  disabled: "outline",
}

// 秒级年龄按 1h/1m/1s 分档，避免把 7200 秒直接摊给运维看。
function formatAge(seconds: number): string {
  if (seconds >= 3600) return `${Math.round(seconds / 3600)}h`
  if (seconds >= 60) return `${Math.round(seconds / 60)}m`
  return `${Math.max(seconds, 0)}s`
}

function formatValue(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(3).replace(/0+$/, "").replace(/\.$/, "")
}

interface CustomMetricHealthPanelProps {
  health: CustomMetricHealth | null
  nodes?: Node[] | null
}

// CustomMetricHealthPanel 把「最近一次采样及其来源」摊开，便于定位静默采集失败。
export function CustomMetricHealthPanel({ health, nodes }: CustomMetricHealthPanelProps) {
  const { t } = useLocale()
  if (!health) return null
  const nodeName = (nodeId: string) => {
    const node = nodes?.find((candidate) => candidate.id === nodeId)
    return node?.alias || node?.name || nodeId
  }
  const describe = (node: CustomMetricHealthNode) => {
    if (node.state === "missing") return t("custom_metric.health_hint_missing")
    if (node.state === "unverified") return t("custom_metric.health_hint_unverified")
    if (node.state === "disabled") return t("custom_metric.health_hint_disabled")
    const parts = [`${t("custom_metric.health_last_sample")} ${formatAge(node.age_seconds)}`]
    if (node.cadence_ms) parts.push(`${Math.round(node.cadence_ms / 1000)}s`)
    if (node.definition_version) parts.push(node.definition_version)
    if (node.sample_id) parts.push(node.sample_id.slice(0, 8))
    return parts.join(" · ")
  }
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-sm font-medium">{t("custom_metric.health_title")}</h3>
      {health.nodes.length === 0 ? (
        <div className="text-sm text-muted-foreground">
          <p>{t("custom_metric.health_unbound")}</p>
          <p className="text-xs">{t("custom_metric.health_unbound_hint")}</p>
        </div>
      ) : (
        <ul className="flex flex-col gap-1">
          {health.nodes.map((node) => (
            <li key={node.node_id}
              className="flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm">
              <span className="font-medium">{nodeName(node.node_id)}</span>
              <Badge variant={stateVariants[node.state]}>{t(`custom_metric.health_state_${node.state}`)}</Badge>
              {node.value !== undefined && (
                <span className="tabular-nums text-muted-foreground">{formatValue(node.value)}</span>
              )}
              <span className="text-xs text-muted-foreground">{describe(node)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default CustomMetricHealthPanel
