import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { TrafficReportFields } from "@/pages/admin/traffic-report-fields"
import type { TrafficReportForm } from "@/pages/admin/traffic-report-config"
import type { AlertChannel, ManagedNode } from "@/types"

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
  id: "node-1", name: "host-a", alias: "Edge", group_id: "", host: "::1", port: 22,
  status: "online", ssh_public_key: "", cpu_model: "", os: "", platform: "", os_version: "",
  kernel: "", arch: "", virtualization: "", agent_version: "", sort_order: 0, tags: [],
  is_public: true, public_remark: "", private_remark: "", agent_credential_status: "active",
  agent_token_prefix: "beat_", agent_token_created_at: null, agent_token_last_used_at: null,
  agent_token_revoked_at: null, last_seen: "", created_at: "", updated_at: "",
}
const channel: AlertChannel = {
  id: "channel-1", name: "Ops", channel_type: "telegram", config: "{}",
  enabled: true, created_at: "", updated_at: "",
}

function form(overrides: Partial<TrafficReportForm> = {}): TrafficReportForm {
  return {
    name: "Report", cadence: "weekly", timezone: "UTC", sendHour: "9", sendMinute: "15",
    weekday: "5", monthDay: "1", allNodes: false, nodeIds: [node.id],
    allChannels: false, channelIds: [channel.id], enabled: true,
    ...overrides,
  }
}

function view(props: { form: TrafficReportForm; onChange?: (f: TrafficReportForm) => void }) {
  const onChange = props.onChange ?? vi.fn()
  return {
    onChange,
    view: render(<LocaleProvider><TrafficReportFields form={props.form} nodes={[node]} channels={[channel]} t={(key) => key} onChange={onChange} /></LocaleProvider>),
  }
}

describe("TrafficReportFields", () => {
  beforeEach(() => {
    localStorage.setItem("locale", "en")
  })

  it("edits identity, timezone, time, and enabled state", () => {
    const { onChange } = view({ form: form() })
    fireEvent.change(screen.getByLabelText("node.name"), { target: { value: "Renamed" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ name: "Renamed" }))
    fireEvent.change(screen.getByLabelText("traffic_report.timezone"), { target: { value: "Asia/Shanghai" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ timezone: "Asia/Shanghai" }))
    fireEvent.change(screen.getByLabelText("traffic_report.hour"), { target: { value: "10" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ sendHour: "10" }))
    fireEvent.change(screen.getByLabelText("traffic_report.minute"), { target: { value: "30" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ sendMinute: "30" }))
    fireEvent.click(screen.getByRole("switch"))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }))
  })

  it("shows weekday and month-day fields per cadence", () => {
    const weekly = view({ form: form() })
    expect(screen.getAllByTestId("select").length).toBeGreaterThan(0)
    expect(screen.getByLabelText("traffic_report.hour")).toBeInTheDocument()
    weekly.view.unmount()
    const monthly = view({ form: form({ cadence: "monthly" }) })
    expect(screen.getByLabelText("traffic_report.month_day")).toBeInTheDocument()
    monthly.view.unmount()
    view({ form: form({ cadence: "daily" }) })
    expect(screen.queryByLabelText("traffic_report.month_day")).not.toBeInTheDocument()
  })

  it("toggles cadence and target scopes and individual targets", () => {
    const { onChange } = view({ form: form() })
    expect(screen.getByText("Edge (host-a)")).toBeInTheDocument()
    expect(screen.getByText("Ops · alert.channel.telegram")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("checkbox", { name: "Edge (host-a)" }))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({
      nodeIds: [],
    }))
    fireEvent.click(screen.getByRole("button", { name: "traffic_report.cadence.daily" }))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ cadence: "daily" }))
  })

  it("hides target lists when all are selected", () => {
    const { onChange } = view({ form: form({ allNodes: true, allChannels: true }) })
    expect(screen.queryByText("Edge (host-a)")).not.toBeInTheDocument()
    expect(screen.queryByText("Ops · alert.channel.telegram")).not.toBeInTheDocument()
    fireEvent.click(screen.getAllByRole("button", { name: "traffic_report.selected" })[0])
    expect(onChange).toHaveBeenCalled()
  })
})
