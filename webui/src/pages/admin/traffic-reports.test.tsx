import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import TrafficReportsPanel from "@/pages/admin/traffic-reports-panel"
import type { AlertChannel, ManagedNode, TrafficReportSchedule } from "@/types"
import * as hooks from "@/hooks/use-api"
import * as api from "@/lib/api"

vi.mock("@/hooks/use-api")
vi.mock("@/lib/api")
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog,
    DialogContent: mocks.PassThrough,
    DialogDescription: mocks.PassThrough,
    DialogHeader: mocks.PassThrough,
    DialogTitle: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect,
    SelectContent: mocks.PassThrough,
    SelectGroup: mocks.PassThrough,
    SelectItem: mocks.MockSelectItem,
    SelectTrigger: () => null,
    SelectValue: () => null,
  }
})
vi.mock("@/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange, ...props }: {
    checked: boolean
    onCheckedChange?: (checked: boolean) => void
  }) => <button role="switch" aria-checked={checked} onClick={() => onCheckedChange?.(!checked)} {...props}>toggle</button>,
}))
vi.mock("@/components/ui/checkbox", () => ({
  Checkbox: ({ checked, onCheckedChange, ...props }: {
    checked: boolean
    onCheckedChange?: (checked: boolean) => void
  }) => <input type="checkbox" checked={checked} onChange={(event) => onCheckedChange?.(event.target.checked)} {...props} />,
}))
vi.mock("@/components/ui/scroll-area", () => ({
  ScrollArea: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const node: ManagedNode = {
  id: "node-secret-id", name: "beat-host", alias: "Edge", group_id: "", host: "::1", port: 22,
  status: "online", ssh_public_key: "", cpu_model: "", os: "", platform: "", os_version: "",
  kernel: "", arch: "", virtualization: "", agent_version: "", sort_order: 0, tags: [],
  is_public: true, public_remark: "", private_remark: "", agent_credential_status: "active",
  agent_token_prefix: "beat_", agent_token_created_at: null, agent_token_last_used_at: null,
  agent_token_revoked_at: null, last_seen: "", created_at: "", updated_at: "",
}
const channel: AlertChannel = {
  id: "channel-secret-id", name: "Operations", channel_type: "telegram", config: "{}",
  enabled: true, created_at: "", updated_at: "",
}
const schedule: TrafficReportSchedule = {
  id: "schedule-secret-id", name: "Friday summary", cadence: "weekly", timezone: "Asia/Shanghai",
  send_hour: 9, send_minute: 15, weekday: 5, month_day: 1,
  all_nodes: false, node_ids: [node.id], all_channels: false, channel_ids: [channel.id],
  enabled: true, last_run_at: "2026-07-24T01:15:00Z", next_run_at: "2026-07-31T01:15:00Z",
  last_delivery: {
    state: "success", message: "delivered to 1/1 channels", delivered: 1, total: 1,
    delivered_at: "2026-07-24T01:15:00Z",
  },
  created_at: "", updated_at: "",
}

const state = (data: unknown, overrides = {}) => ({
  data, loading: false, error: null, refresh: vi.fn().mockResolvedValue(undefined), ...overrides,
})

function view() {
  return render(<LocaleProvider><MemoryRouter><TrafficReportsPanel /></MemoryRouter></LocaleProvider>)
}

describe("traffic report schedules", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.setItem("locale", "en")
    vi.mocked(hooks.useTrafficReportSchedules).mockReturnValue(state([schedule]) as never)
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([node]) as never)
    vi.mocked(hooks.useAlertChannels).mockReturnValue(state([channel]) as never)
  })

  it("renders readable cadence and scope labels without exposing IDs", () => {
    view()
    expect(screen.getByText("Friday summary")).toBeInTheDocument()
    expect(screen.getByText(/Weekly · Friday · 09:15 · Asia\/Shanghai/)).toBeInTheDocument()
    expect(screen.getByText(/Report nodes: Edge \(beat-host\)/)).toBeInTheDocument()
    expect(screen.getByText(/Delivery channels: Operations/)).toBeInTheDocument()
    expect(screen.queryByText("node-secret-id")).not.toBeInTheDocument()
    expect(screen.queryByText("channel-secret-id")).not.toBeInTheDocument()
    expect(screen.getByText("Delivered · 1/1")).toBeInTheDocument()
  })

  it("toggles, tests, edits, and deletes schedules", async () => {
    vi.mocked(api.testTrafficReportSchedule).mockResolvedValue({
      report: { schedule_id: schedule.id, schedule_name: schedule.name, cadence: "weekly", timezone: "Asia/Shanghai" },
      delivery: {
        state: "partial", message: "delivered to 1/2 channels", delivered: 1, total: 2,
        delivered_at: "2026-07-30T00:00:00Z",
      },
    })
    view()
    fireEvent.click(screen.getByRole("switch"))
    await waitFor(() => expect(api.updateTrafficReportSchedule).toHaveBeenCalledWith(
      schedule.id,
      expect.objectContaining({ enabled: false, node_ids: [node.id], channel_ids: [channel.id] }),
    ))
    const row = within(screen.getByText("Friday summary").closest("tr")!)
    fireEvent.click(row.getByRole("button", { name: "Test report" }))
    expect(await screen.findByText("Partially delivered · 1/2")).toBeInTheDocument()

    fireEvent.click(row.getByRole("button", { name: "Edit" }))
    expect(screen.getByRole("dialog")).toBeInTheDocument()
    expect(screen.getByDisplayValue("Friday summary")).toBeInTheDocument()
    expect(screen.getByTestId("select")).toHaveAttribute("data-selected-label", "Friday")
    expect(screen.getByText("Edge (beat-host)")).toBeInTheDocument()
    expect(screen.getByText("Operations · Telegram")).toBeInTheDocument()
    fireEvent.change(screen.getByDisplayValue("Friday summary"), { target: { value: "Weekly changed" } })
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.updateTrafficReportSchedule).toHaveBeenCalledWith(
      schedule.id,
      expect.objectContaining({ name: "Weekly changed", weekday: 5 }),
    ))

    fireEvent.click(row.getByRole("button", { name: "Delete" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(api.deleteTrafficReportSchedule).toHaveBeenCalledWith(schedule.id))
  })

  it("creates schedules and reports validation errors", async () => {
    view()
    fireEvent.click(screen.getByRole("button", { name: "Create report" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save" }))
    expect(screen.getByText("Enter a report name.")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Daily summary" } })
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.createTrafficReportSchedule).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Daily summary", cadence: "daily", all_nodes: true, all_channels: true }),
    ))
  })

  it("renders loading, empty, and fetch error states", () => {
    vi.mocked(hooks.useTrafficReportSchedules).mockReturnValue(state([], { loading: true }) as never)
    const rendered = view()
    expect(document.querySelectorAll("tbody tr")).toHaveLength(2)
    rendered.unmount()
    vi.mocked(hooks.useTrafficReportSchedules).mockReturnValue(state([]) as never)
    expect(view().getByText("No data")).toBeInTheDocument()
    vi.mocked(hooks.useTrafficReportSchedules).mockReturnValue(state([], { error: "reports failed" }) as never)
    expect(view().getByText("reports failed")).toBeInTheDocument()
  })
})
