import type { Catalog } from "@/context/locales"

export const telemetryTranslations: Catalog = {
  en: {
    "telemetry.fresh": "Metrics current",
    "telemetry.delayed": "Metrics delayed",
    "telemetry.stale": "Metrics stale",
    "telemetry.unavailable": "No confirmed metrics",
    "telemetry.unavailableDescription": "No accepted sample is available",
    "telemetry.age": "{seconds}s ago",
  },
}
