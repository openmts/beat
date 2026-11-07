import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { DashboardView } from "@/pages/dashboard-view"
import { workspaceNode } from "@/test/metric-workspace-fixtures"

const state = vi.hoisted(() => ({ theme: true }))

vi.mock("@/components/header", () => ({ Header: () => null }))
vi.mock("@/components/public-footer", () => ({ PublicFooter: () => null }))
vi.mock("@/components/group-tabs", () => ({ GroupTabs: () => null }))
vi.mock("@/components/node-card", () => ({
  NodeCard: ({ network }: { network?: { latency_ms: number; loss_percent: number } }) =>
    <p data-testid="node-card">{network ? `${network.latency_ms}/${network.loss_percent}` : "none"}</p>,
}))
vi.mock("@/components/themed-node-collection", () => ({
  ThemedNodeCollection: ({ networkSummaries }: { networkSummaries?: Record<string, { latency_ms: number; loss_percent: number }> }) =>
    <p data-testid="themed-nodes">{networkSummaries?.node ? `${networkSummaries.node.latency_ms}/${networkSummaries.node.loss_percent}` : "none"}</p>,
}))
vi.mock("@/context/locale", () => ({ useLocale: () => ({ t: (key: string) => key }) }))
vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({ settings: { show_network_quality: true }, loading: false, error: null }),
}))
vi.mock("@/hooks/use-network", () => ({
  useNodeNetworkSummaries: () => ({
    summaries: {
      node: {
        task_id: "task", task_name: "Probe", direction: "server_to_agent", type: "icmp",
        latency_ms: 12, loss_percent: 5, success: true, timestamp: "2026-11-07T00:00:00Z",
      },
    },
  }),
}))
vi.mock("@/lib/theme-presentation", () => ({ usePublicTheme: () => state.theme ? {} : null }))

const props = {
  nodes: [workspaceNode("node")],
  groups: [],
  nodesLoading: false,
  groupsLoading: false,
  nodesError: null,
  groupsError: null,
  selectedGroup: "",
  onGroupChange: vi.fn(),
}

describe("DashboardView network summaries", () => {
  it("passes summaries to custom themed nodes", () => {
    state.theme = true
    render(<DashboardView {...props} />)
    expect(screen.getByTestId("themed-nodes")).toHaveTextContent("12/5")
  })

  it("passes summaries to default node cards", () => {
    state.theme = false
    render(<DashboardView {...props} />)
    expect(screen.getByTestId("node-card")).toHaveTextContent("12/5")
  })
})
