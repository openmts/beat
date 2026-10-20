import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"
import { TelemetryStatus } from "@/components/telemetry-status"
import { LocaleProvider } from "@/context/locale"
import type { Node, TelemetryState } from "@/types"

function renderStatus(node: Partial<Node>) {
  return render(<LocaleProvider><TelemetryStatus node={node} className="test-status" /></LocaleProvider>)
}

beforeEach(() => { localStorage.setItem("locale", "en") })

describe("telemetry status", () => {
  it.each<[TelemetryState, string]>([["fresh", "Metrics current"], ["delayed", "Metrics delayed"], ["stale", "Metrics stale"]])("shows %s independently of contact status", (state, label) => {
    renderStatus({ status: "online", telemetry_state: state, telemetry_age_seconds: 41.9 })
    expect(screen.getByLabelText(`${label} · 41s ago`)).toHaveClass("test-status")
  })
  it("does not invent an age for absent receipts", () => {
    renderStatus({})
    expect(screen.getByText("No confirmed metrics")).toHaveAttribute("title", "No accepted sample is available")
  })
  it.each([null, undefined, -1, Infinity, NaN])("does not display invalid age %s", (age) => {
    renderStatus({ telemetry_state: "stale", telemetry_age_seconds: age })
    expect(screen.getByText("Metrics stale")).toHaveAttribute("title", "No accepted sample is available")
  })
  // 简体中文目录按语言分片懒加载，首屏恢复要等分片到位后再断言文案。
  it("uses the Chinese locale", async () => {
    localStorage.setItem("locale", "zh-CN")
    renderStatus({ telemetry_state: "delayed", telemetry_age_seconds: 50 })
    expect(await screen.findByLabelText("指标延迟 · 50 秒前")).toBeInTheDocument()
  })
})
