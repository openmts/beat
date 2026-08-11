import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import Groups from "@/pages/admin/groups"
import Nodes from "@/pages/admin/nodes"
import * as hooks from "@/hooks/use-api"
import * as api from "@/lib/api"

vi.mock("@/hooks/use-api")
vi.mock("@/lib/api")
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog, DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough, DialogTitle: mocks.PassThrough,
    DialogDescription: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect, SelectContent: mocks.PassThrough,
    SelectGroup: mocks.PassThrough, SelectItem: mocks.MockSelectItem,
    SelectTrigger: () => null, SelectValue: () => null,
  }
})

const state = (data: unknown, overrides = {}) => ({
  data, loading: false, error: null, refresh: vi.fn().mockResolvedValue(undefined), ...overrides,
})
const group = (id: string, name: string, isDefault = false) => ({
  id, name, is_default: isDefault, sort_order: 0, created_at: "", updated_at: "",
})
const node = {
  id: "n1", name: "server-one", alias: "Primary", group_id: "g1",
  host: "127.0.0.1", port: 22, status: "online", ssh_public_key: "pub",
  last_seen: "", created_at: "", updated_at: "",
  agent_version: "1.0.0", agent_credential_status: "active",
  agent_token_prefix: "beat_agent_v1_abcd", agent_token_created_at: "",
  agent_token_last_used_at: "", agent_token_revoked_at: null,
  sort_order: 0, tags: ["edge", "prod"], is_public: true,
  public_remark: "Customer edge", private_remark: "Rack 2",
  traffic_limit: 100 * 1024 ** 3, traffic_limit_type: "sum", traffic_reset_day: 31,
  traffic: {
    sent: 20 * 1024 ** 3, received: 30 * 1024 ** 3, used: 50 * 1024 ** 3,
    limit: 100 * 1024 ** 3, remaining: 50 * 1024 ** 3, percentage: 50,
    limit_type: "sum", reset_day: 31, period_start: "2026-07-01T00:00:00Z",
    next_reset: "2026-07-31T00:00:00Z", tracked_since: "2026-07-02T00:00:00Z",
    status: "normal",
  },
  metrics: {
    cpu: 12.34, cpu_used: 0.9872, cpu_total: 8,
    memory: 56.78, memory_used: 8 * 1024 ** 3, memory_total: 16 * 1024 ** 3,
    disk_used: 40 * 1024 ** 3, disk_total: 120 * 1024 ** 3,
  },
}

function view(element: React.ReactNode) {
  return render(<LocaleProvider><MemoryRouter>{element}</MemoryRouter></LocaleProvider>)
}

describe("admin groups", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(hooks.useGroups).mockReturnValue(state([
      group("g1", "Default", true), group("g2", "Other"),
    ]) as never)
  })

  it("renders errors, loading, and empty state", () => {
    vi.mocked(hooks.useGroups).mockReturnValue(state([], { error: "groups failed" }) as never)
    const { rerender } = view(<Groups />)
    expect(screen.getByText("groups failed")).toBeInTheDocument()
    vi.mocked(hooks.useGroups).mockReturnValue(state([], { loading: true }) as never)
    rerender(<LocaleProvider><MemoryRouter><Groups /></MemoryRouter></LocaleProvider>)
    expect(document.querySelectorAll("tbody tr")).toHaveLength(3)
    vi.mocked(hooks.useGroups).mockReturnValue(state([]) as never)
    rerender(<LocaleProvider><MemoryRouter><Groups /></MemoryRouter></LocaleProvider>)
    expect(screen.getByText("No data")).toBeInTheDocument()
  })

  it("creates, renames, sorts, defaults, and deletes groups", async () => {
    const refresh = vi.fn().mockResolvedValue(undefined)
    vi.mocked(hooks.useGroups).mockReturnValue({ ...state([]), data: [
      group("g1", "Default", true), group("g2", "Other"),
    ], refresh } as never)
    view(<Groups />)
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    fireEvent.change(screen.getByPlaceholderText("Group Name"), { target: { value: " New " } })
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.createGroup).toHaveBeenCalledWith("New"))

    const otherRow = screen.getByText("Other").closest("tr")!
    const buttons = within(otherRow).getAllByRole("button")
    fireEvent.click(buttons[0])
    fireEvent.click(within(screen.getByText("Default").closest("tr")!).getAllByRole("button")[1])
    fireEvent.click(buttons[2])
    fireEvent.change(screen.getByDisplayValue("Other"), { target: { value: "Renamed" } })
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.updateGroup).toHaveBeenCalledWith("g2", "Renamed"))
    fireEvent.click(buttons[3])
    await waitFor(() => expect(api.setDefaultGroup).toHaveBeenCalledWith("g2"))
    fireEvent.click(buttons[4])
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(api.deleteGroup).toHaveBeenCalledWith("g2"))
    expect(api.updateGroupSort).toHaveBeenCalledTimes(2)
    expect(refresh).toHaveBeenCalled()
  })

  it("shows action failures", async () => {
    vi.mocked(api.createGroup).mockRejectedValue("failure")
    view(<Groups />)
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    fireEvent.change(screen.getByPlaceholderText("Group Name"), { target: { value: "New" } })
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    expect(await screen.findByText("The request failed")).toBeInTheDocument()
  })
})

describe("admin nodes", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([node]) as never)
    vi.mocked(hooks.useGroups).mockReturnValue(state([group("g1", "Default")]) as never)
    vi.mocked(hooks.useSSHKeys).mockReturnValue(state([{
      id: "k1", name: "Managed", public_key: "pub", key_type: "ed25519",
      fingerprint: "fp", created_at: "",
    }]) as never)
  })

  it("renders states and searches nodes", () => {
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([], { error: "nodes failed" }) as never)
    const { rerender } = view(<Nodes />)
    expect(screen.getByText("nodes failed")).toBeInTheDocument()
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([node], { loading: true }) as never)
    rerender(<LocaleProvider><MemoryRouter><Nodes /></MemoryRouter></LocaleProvider>)
    expect(screen.getAllByTestId("node-card-skeleton")).toHaveLength(6)
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([node]) as never)
    rerender(<LocaleProvider><MemoryRouter><Nodes /></MemoryRouter></LocaleProvider>)
    fireEvent.change(screen.getByPlaceholderText("Search"), { target: { value: "missing" } })
    expect(screen.getByText("No data")).toBeInTheDocument()
  })

  it("groups node cards by group and keeps unassigned nodes separate", () => {
    vi.mocked(hooks.useGroups).mockReturnValue(state([
      group("g1", "Default"), group("g2", "Other"),
    ]) as never)
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([
      node,
      { ...node, id: "n2", alias: "Secondary", group_id: "g2", status: "offline" },
      { ...node, id: "n3", alias: "Standalone", group_id: "", ssh_public_key: "" },
    ]) as never)
    view(<Nodes />)

    expect(within(screen.getByRole("region", { name: "Default" })).getByText("Primary"))
      .toBeInTheDocument()
    const otherGroup = screen.getByRole("region", { name: "Other" })
    expect(within(otherGroup).getByText("Secondary")).toBeInTheDocument()
    expect(within(otherGroup).getByText("Offline")).toBeInTheDocument()
    const unassignedGroup = screen.getByRole("region", { name: "Not assigned" })
    expect(within(unassignedGroup).getByText("Standalone")).toBeInTheDocument()
    expect(within(screen.getByTestId("node-card-n3")).getByText("Not assigned"))
      .toBeInTheDocument()
    expect(document.querySelector("table")).not.toBeInTheDocument()
  })

  it("shows usage metrics and wraps long IPv6 endpoints", () => {
    const ipv6 = "2408:8256:3384:10a:fb76:d511:38e0:abdf"
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([{ ...node, host: ipv6 }]) as never)
    view(<Nodes />)

    const card = screen.getByTestId("node-card-n1")
    expect(within(card).getByText("1 / 8 cores")).toBeInTheDocument()
    expect(within(card).getByText("12.3%")).toBeInTheDocument()
    expect(within(card).getByText("8 GiB / 16 GiB")).toBeInTheDocument()
    expect(within(card).getByText("56.8%")).toBeInTheDocument()
    expect(within(card).getByText("40 GiB / 120 GiB")).toBeInTheDocument()
    expect(within(card).getByText("50 GiB / 100 GiB")).toBeInTheDocument()
    expect(within(card).getByText("Normal")).toBeInTheDocument()
    expect(within(card).getByTitle(`${ipv6}:22`)).toHaveClass("break-all")
  })

  it("updates and deletes a node", async () => {
    view(<Nodes />)
    const card = screen.getByTestId("node-card-n1")
    fireEvent.click(within(card).getByRole("button", { name: "Edit" }))
    fireEvent.change(screen.getByDisplayValue("Primary"), { target: { value: "Changed" } })
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "Default")
    expect(selects[1]).toHaveAttribute("data-selected-label", "Managed")
    expect(selects[2]).toHaveAttribute("data-selected-label", "Upload + download")
    expect(screen.queryByText("sum")).not.toBeInTheDocument()
    fireEvent.change(selects[1], { target: { value: "none" } })
    fireEvent.change(screen.getByLabelText("Monthly quota (GiB)"), { target: { value: "200" } })
    fireEvent.change(screen.getByLabelText("Reset day"), { target: { value: "15" } })
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "edge, critical" } })
    fireEvent.change(screen.getByLabelText("Public remark"), { target: { value: "Public context" } })
    fireEvent.change(screen.getByLabelText("Private remark"), { target: { value: "Private context" } })
    fireEvent.change(screen.getByLabelText("Display order"), { target: { value: "3" } })
    fireEvent.click(screen.getByRole("switch", { name: "Public visibility" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.updateNode).toHaveBeenCalledWith("n1", {
      alias: "Changed", group_id: "g1", ssh_public_key: "",
      traffic_limit: 200 * 1024 ** 3, traffic_limit_type: "sum", traffic_reset_day: 15,
      sort_order: 3, tags: ["edge", "critical"], is_public: false,
      public_remark: "Public context", private_remark: "Private context",
    }))
    fireEvent.click(within(card).getByRole("button", { name: "Actions" }))
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(api.deleteNode).toHaveBeenCalledWith("n1"))
  })

  it("shows presentation state and reorders nodes within a group", async () => {
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([
      node,
      { ...node, id: "n2", name: "server-two", alias: "Secondary", sort_order: 1 },
    ]) as never)
    view(<Nodes />)
    const firstCard = screen.getByTestId("node-card-n1")
    expect(within(firstCard).getByText("Public")).toBeInTheDocument()
    expect(within(firstCard).getByText("edge")).toBeInTheDocument()
    expect(within(firstCard).getByText("Customer edge")).toBeInTheDocument()
    expect(within(firstCard).getByText("Rack 2")).toBeInTheDocument()
    fireEvent.click(within(firstCard).getByRole("button", { name: "Actions" }))
    fireEvent.click(await screen.findByRole("menuitem", { name: "Move down" }))
    await waitFor(() => expect(api.updateNodeSort).toHaveBeenCalledWith(["n2", "n1"]))
  })

  it("rejects invalid traffic policy values before saving", () => {
    view(<Nodes />)
    fireEvent.click(within(screen.getByTestId("node-card-n1")).getByRole("button", { name: "Edit" }))
    const save = screen.getByRole("button", { name: "Save" })
    fireEvent.change(screen.getByLabelText("Monthly quota (GiB)"), { target: { value: "-1" } })
    expect(save).toBeDisabled()
    expect(screen.getByText("Enter a non-negative quota.")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Monthly quota (GiB)"), { target: { value: "1" } })
    fireEvent.change(screen.getByLabelText("Reset day"), { target: { value: "32" } })
    expect(save).toBeDisabled()
    expect(screen.getByText("Enter a day from 1 to 31.")).toBeInTheDocument()
  })

  it("shows key and action errors", async () => {
    vi.mocked(hooks.useSSHKeys).mockReturnValue(state([], { error: "keys failed" }) as never)
    vi.mocked(api.deleteNode).mockRejectedValue(new Error("delete failed"))
    view(<Nodes />)
    expect(screen.getByText("keys failed")).toBeInTheDocument()
    fireEvent.click(within(screen.getByTestId("node-card-n1")).getByRole("button", { name: "Actions" }))
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
    expect(await screen.findByText("delete failed")).toBeInTheDocument()
  })

  it("creates a node and clears one-time credentials on close", async () => {
    vi.mocked(api.createManagedNode).mockResolvedValue({
      node,
      agent_token: "beat_agent_v1_secret",
      agent_config: {
        server_url: window.location.origin,
        agent_token: "beat_agent_v1_secret",
        node_name: "new-node",
        advertised_host: "2001:db8::1",
        ssh_port: 22,
        report_interval: "5s",
      },
    } as never)
    view(<Nodes />)
    fireEvent.click(screen.getByRole("button", { name: "Create node" }))
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "Not assigned")
    expect(selects[1]).toHaveAttribute("data-selected-label", "Not assigned")
    fireEvent.change(selects[0], { target: { value: "g1" } })
    fireEvent.change(selects[1], { target: { value: "pub" } })
    expect(selects[0]).toHaveAttribute("data-selected-label", "Default")
    expect(selects[1]).toHaveAttribute("data-selected-label", "Managed")
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: " new-node " } })
    fireEvent.change(screen.getByLabelText("Host"), { target: { value: "2001:db8::1" } })
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create" }))
    await waitFor(() => expect(api.createManagedNode).toHaveBeenCalledWith({
      name: "new-node",
      alias: "",
      group_id: "g1",
      host: "2001:db8::1",
      port: 22,
      ssh_public_key: "pub",
      server_url: window.location.origin,
    }))
    expect(await screen.findByText("beat_agent_v1_secret")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    expect(screen.queryByText("beat_agent_v1_secret")).not.toBeInTheDocument()
  })

  it("rotates and revokes node credentials", async () => {
    vi.mocked(api.rotateAgentToken).mockResolvedValue({
      node,
      agent_token: "beat_agent_v1_rotated",
      agent_config: {
        server_url: window.location.origin,
        agent_token: "beat_agent_v1_rotated",
        node_name: node.name,
        advertised_host: node.host,
        ssh_port: node.port,
        report_interval: "5s",
      },
    } as never)
    view(<Nodes />)
    const actions = within(screen.getByTestId("node-card-n1")).getByRole("button", { name: "Actions" })
    fireEvent.click(actions)
    fireEvent.click(await screen.findByRole("menuitem", { name: "Rotate token" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Rotate token" }))
    await waitFor(() => expect(api.rotateAgentToken).toHaveBeenCalledWith("n1", window.location.origin))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))

    fireEvent.click(actions)
    fireEvent.click(await screen.findByRole("menuitem", { name: "Revoke token" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Revoke token" }))
    await waitFor(() => expect(api.revokeAgentToken).toHaveBeenCalledWith("n1"))
  })
})
