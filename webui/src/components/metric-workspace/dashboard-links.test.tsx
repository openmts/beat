import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { WorkspaceDashboardLinks } from "./dashboard-links"
import type { DashboardDefinition } from "@/types/metric-workspace"

const definition: DashboardDefinition = {
  version: 1, title: "Probe", description: "", time_range_seconds: 3600, refresh_seconds: 0, variables: [], panels: [],
  links: [{ title: "Runbook", url: "https://wiki.example.test/beat" }],
}

describe("dashboard link bar", () => {
  it("renders the links with the resolved placeholders", () => {
    render(<LocaleProvider><WorkspaceDashboardLinks definition={definition} values={{}} /></LocaleProvider>)
    expect(screen.getByRole("navigation", { name: "Probe" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Runbook" })).toHaveAttribute("href", "https://wiki.example.test/beat")
  })

  it("keeps the bar in the editable workspace even when the switch is on", () => {
    render(<LocaleProvider><WorkspaceDashboardLinks definition={{ ...definition, hide_links: true }} values={{}} /></LocaleProvider>)
    expect(screen.getByRole("link", { name: "Runbook" })).toBeInTheDocument()
  })

  it("hides the bar in read-only views when the switch is on", () => {
    const { rerender } = render(<LocaleProvider><WorkspaceDashboardLinks readOnly definition={definition} values={{}} /></LocaleProvider>)
    expect(screen.getByRole("link", { name: "Runbook" })).toBeInTheDocument()
    rerender(<LocaleProvider><WorkspaceDashboardLinks readOnly definition={{ ...definition, hide_links: true }} values={{}} /></LocaleProvider>)
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument()
  })

  it("renders nothing without links", () => {
    render(<LocaleProvider><WorkspaceDashboardLinks readOnly definition={{ ...definition, links: [] }} values={{}} /></LocaleProvider>)
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument()
  })
})
