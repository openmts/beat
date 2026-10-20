import { render } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it, vi } from "vitest"
import { PublicTelemetryTracker } from "@/components/public-telemetry-tracker"

const report = vi.fn()
vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({ reportInteraction: report }),
}))

describe("public telemetry tracker", () => {
  it("records a coarse page view for public routes", () => {
    render(<MemoryRouter initialEntries={["/node/node-one"]}><PublicTelemetryTracker /></MemoryRouter>)
    expect(report).toHaveBeenCalledWith("page_view", "node")
  })

  it("never reports administration routes", () => {
    render(<MemoryRouter initialEntries={["/admin/settings"]}><PublicTelemetryTracker /></MemoryRouter>)
    expect(report).not.toHaveBeenCalled()
  })
})
