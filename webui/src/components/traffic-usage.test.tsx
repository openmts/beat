import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TrafficUsage } from "@/components/traffic-usage"
import { LocaleProvider } from "@/context/locale"
import type { TrafficStatus, TrafficSummary } from "@/types"

const gib = 1024 ** 3

function traffic(status: TrafficStatus, percentage: number | null): TrafficSummary {
  const limit = percentage === null ? 0 : 100 * gib
  const used = percentage === null ? 5 * gib : percentage * gib
  return {
    sent: used * 0.4,
    received: used * 0.6,
    used,
    limit,
    remaining: percentage === null ? null : Math.max(0, limit - used),
    percentage,
    limit_type: "sum",
    reset_day: 1,
    period_start: "2026-07-01T00:00:00Z",
    next_reset: "2026-08-01T00:00:00Z",
    tracked_since: percentage === null ? null : "2026-07-02T00:00:00Z",
    status,
  }
}

describe("traffic usage", () => {
  it("renders unlimited usage and missing tracking state", () => {
    render(<LocaleProvider><TrafficUsage traffic={traffic("unlimited", null)} /></LocaleProvider>)
    expect(screen.getByText("5 GiB · Unlimited")).toBeInTheDocument()
    expect(screen.getByText("Waiting for traffic data")).toBeInTheDocument()
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
  })

  it.each([
    ["normal", 50, "Normal", "50.0% · 50 GiB remaining"],
    ["warning", 75, "Warning", "75.0% · 25 GiB remaining"],
    ["critical", 95, "Critical", "95.0% · 5 GiB remaining"],
    ["exceeded", 110, "Exceeded", "110.0% · 0 B remaining"],
  ] as const)("renders %s quota status", (status, percentage, label, detail) => {
    render(<LocaleProvider><TrafficUsage traffic={traffic(status, percentage)} /></LocaleProvider>)
    expect(screen.getByText(label)).toBeInTheDocument()
    expect(screen.getByText(detail)).toBeInTheDocument()
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-label", `${Math.min(percentage, 100).toFixed(1)}%`)
  })
})
