import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { CustomMetricHealthPanel } from "@/components/custom-metric-health"
import type { CustomMetricHealth, Node } from "@/types"

const health = (nodes: CustomMetricHealth["nodes"]): CustomMetricHealth => ({
  metric: { id: "m1", name: "probe", enabled: true, interval_seconds: 30, stale_after_seconds: 90 },
  nodes,
})

const node = {
  id: "n1", name: "Node One", alias: "Primary", group_id: "", host: "host", port: 22,
  status: "online", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
} as Node

function renderPanel(value: CustomMetricHealth | null, nodes: Node[] = [node]) {
  return render(
    <LocaleProvider>
      <CustomMetricHealthPanel health={value} nodes={nodes} />
    </LocaleProvider>,
  )
}

describe("custom metric health panel", () => {
  it("renders nothing without health data", () => {
    const { container } = renderPanel(null)
    expect(container).toBeEmptyDOMElement()
  })

  it("lists bound nodes with provenance detail", () => {
    renderPanel(health([
      { node_id: "n1", state: "ok", value: 12.5, age_seconds: 42, cadence_ms: 30000,
        definition_version: "v2", sample_id: "abcdefgh-1111-4111-8111-111111111111" },
      { node_id: "n2", state: "stale", value: 3, age_seconds: 7200, cadence_ms: 60000 },
      { node_id: "n3", state: "unverified", age_seconds: 0 },
      { node_id: "n4", state: "disabled", age_seconds: 0 },
    ]))
    expect(screen.getByText("Primary")).toBeInTheDocument()
    expect(screen.getByText("Collecting")).toBeInTheDocument()
    expect(screen.getByText("12.5")).toBeInTheDocument()
    expect(screen.getByText(/Last sample 42s · 30s · v2 · abcdefgh/)).toBeInTheDocument()
    expect(screen.getByText("Stale")).toBeInTheDocument()
    expect(screen.getByText(/Last sample 2h/)).toBeInTheDocument()
    expect(screen.getByText("Unverified")).toBeInTheDocument()
    expect(screen.getByText(/no provenance receipt/)).toBeInTheDocument()
    expect(screen.getByText("Disabled")).toBeInTheDocument()
    expect(screen.getByText(/Collection is paused/)).toBeInTheDocument()
    // 未绑定的节点没有别名时回退到节点 ID。
    expect(screen.getByText("n2")).toBeInTheDocument()
  })

  it("explains an unbound metric", () => {
    renderPanel(health([]))
    expect(screen.getByText(/not bound to any node/)).toBeInTheDocument()
    expect(screen.getByText(/Bind the metric to a node/)).toBeInTheDocument()
  })
})
