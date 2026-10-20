import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceAllSelection } from "@/lib/workspace-variable-options"
import { WorkspaceVariables } from "./variable-controls"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import type { Group, Node } from "@/types"

const groups: Group[] = [{ id: "group-one", name: "Production", sort_order: 0, is_default: true, created_at: "", updated_at: "" }]

function nodes(): Node[] {
  const second = workspaceNode("node-two")
  second.alias = "East host"
  return [workspaceNode(), second]
}

function draw(props: { multi?: boolean; includeAll?: boolean; regex?: string; values?: Record<string, string[]>; nodes?: Node[] }) {
  const onChange = vi.fn()
  render(<LocaleProvider><WorkspaceVariables variables={[{ name: "node", type: "node", label: "Node", multi: props.multi, include_all: props.includeAll, regex: props.regex }]}
    values={props.values ?? { node: ["node-one"] }} nodes={props.nodes ?? nodes()} groups={groups} onChange={onChange} /></LocaleProvider>)
  return onChange
}

describe("workspace variable controls", () => {
  it("reports a single selection as a one-element list", async () => {
    const user = userEvent.setup()
    const onChange = draw({ values: { node: ["node-one"] } })
    await user.click(screen.getByRole("combobox", { name: "Node" }))
    await user.click(await screen.findByRole("option", { name: "East host" }))
    expect(onChange).toHaveBeenCalledWith("node", ["node-two"])
  })

  it("selects and clears several values for a multi variable", async () => {
    const user = userEvent.setup()
    const onChange = draw({ multi: true })
    await user.click(screen.getByRole("button", { name: "Node" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(within(dialog).getByRole("checkbox", { name: "East host" }))
    expect(onChange).toHaveBeenLastCalledWith("node", ["node-one", "node-two"])
    await user.click(within(dialog).getByRole("checkbox", { name: "Production host" }))
    expect(onChange).toHaveBeenLastCalledWith("node", [])
  })

  it("offers All only for an opted-in multi variable and selects the sentinel", async () => {
    const user = userEvent.setup()
    const onChange = draw({ multi: true, includeAll: true })
    await user.click(screen.getByRole("button", { name: "Node" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(within(dialog).getByRole("checkbox", { name: "All" }))
    expect(onChange).toHaveBeenLastCalledWith("node", [workspaceAllSelection])
    draw({ multi: true, includeAll: true, values: { node: [workspaceAllSelection] } })
    expect(screen.getAllByRole("button", { name: "Node" }).at(-1)).toHaveTextContent("All")
  })

  it("summarises more than two selections with a counter", () => {
    const third = workspaceNode("node-three")
    third.alias = "South host"
    draw({ multi: true, nodes: [...nodes(), third], values: { node: ["node-one", "node-two", "node-three"] } })
    expect(screen.getByRole("button", { name: "Node" })).toHaveTextContent("Production host, East host +1 more")
  })

  it("narrows options with the search box and hides filtered labels in the summary", async () => {
    const user = userEvent.setup()
    draw({ multi: true, regex: "^East", values: { node: ["node-two"] } })
    expect(screen.getByRole("button", { name: "Node" })).toHaveTextContent("East host")
    await user.click(screen.getByRole("button", { name: "Node" }))
    const dialog = await screen.findByRole("dialog")
    expect(within(dialog).queryByRole("checkbox", { name: "Production host" })).toBeNull()
    await user.type(within(dialog).getByPlaceholderText("Search"), "missing")
    expect(await within(dialog).findByText("No samples in this range")).toBeInTheDocument()
  })
})
