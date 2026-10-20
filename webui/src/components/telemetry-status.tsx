import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { cn } from "@/lib/utils"
import type { Node, TelemetryState } from "@/types"

type TelemetryNode = Pick<Node, "telemetry_at" | "telemetry_age_seconds" | "telemetry_state">

const variants = { fresh: "secondary", delayed: "outline", stale: "destructive", unavailable: "outline" } as const

export function TelemetryStatus({ node, className }: { node: TelemetryNode; className?: string }) {
  const { t } = useLocale()
  const state: TelemetryState = node.telemetry_state ?? "unavailable"
  const age = node.telemetry_age_seconds
  const hasAge = typeof age === "number" && Number.isFinite(age) && age >= 0
  const description = state === "unavailable" || !hasAge
    ? t("telemetry.unavailableDescription")
    : t("telemetry.age", { seconds: Math.floor(age) })
  return (
    <Badge variant={variants[state]} className={cn("max-w-full", className)} title={description} aria-label={`${t(`telemetry.${state}`)} · ${description}`}>
      {t(`telemetry.${state}`)}
      {state !== "unavailable" && hasAge && <span className="font-normal">· {description}</span>}
    </Badge>
  )
}
