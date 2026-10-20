import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import CustomMetrics from "@/pages/admin/custom-metrics"
import * as hooks from "@/hooks/use-api"
import * as api from "@/lib/api"

vi.mock("@/hooks/use-api")
vi.mock("@/lib/api")
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog, DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough, DialogTitle: mocks.PassThrough,
    DialogDescription: mocks.PassThrough, DialogFooter: mocks.PassThrough,
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
vi.mock("@/components/metrics-chart", () => ({
  MetricsChart: () => <div data-testid="metrics-chart" />,
}))

const state = (data: unknown, overrides = {}) => ({
  data, loading: false, error: null, refresh: vi.fn().mockResolvedValue(undefined), ...overrides,
})

const metric = {
  id: "m1",
  name: "http_latency",
  shell_command: "echo 5",
  interval_seconds: 30,
  description: "latency",
  enabled: true,
  created_at: "",
  updated_at: "",
}

const node = {
  id: "n1", name: "Node One", alias: "Primary", group_id: "", host: "host", port: 22,
  status: "online", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
}

function view() {
  return render(<LocaleProvider><MemoryRouter><CustomMetrics /></MemoryRouter></LocaleProvider>)
}

describe("custom metrics", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(hooks.useNodes).mockReturnValue(state([node]) as never)
    vi.mocked(api.listCustomMetrics).mockResolvedValue([metric])
  })

  it("lists custom metrics", async () => {
    view()
    expect(await screen.findByText("http_latency")).toBeInTheDocument()
    expect(screen.getByText("echo 5")).toBeInTheDocument()
  })

  it("creates a custom metric", async () => {
    vi.mocked(api.createCustomMetric).mockResolvedValue(metric)
    view()
    fireEvent.click(await screen.findByRole("button", { name: "New Metric" }))
    fireEvent.change(screen.getByLabelText("Metric name"), { target: { value: "new_metric" } })
    fireEvent.change(screen.getByLabelText("Shell command"), { target: { value: "echo 42" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.createCustomMetric).toHaveBeenCalledWith(
      expect.objectContaining({ name: "new_metric", shell_command: "echo 42" }),
    ))
  })

  it("deletes a custom metric", async () => {
    vi.mocked(api.deleteCustomMetric).mockResolvedValue(undefined)
    view()
    const rowDelete = await screen.findAllByLabelText("Delete")
    fireEvent.click(rowDelete[0])
    const confirm = await screen.findAllByRole("button", { name: "Delete" })
    fireEvent.click(confirm[confirm.length - 1])
    await waitFor(() => expect(api.deleteCustomMetric).toHaveBeenCalledWith("m1"))
  })

  it("binds nodes to a custom metric", async () => {
    vi.mocked(api.listCustomMetricNodes).mockResolvedValue(["n1"])
    vi.mocked(api.setCustomMetricNodes).mockResolvedValue(["n1"])
    view()
    fireEvent.click(await screen.findByRole("button", { name: "Bind nodes" }))
    await waitFor(() => expect(api.listCustomMetricNodes).toHaveBeenCalledWith("m1"))
    // The bound node is pre-selected; save should send the same set.
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0])
    await waitFor(() => expect(api.setCustomMetricNodes).toHaveBeenCalledWith("m1", ["n1"]))
  })

  it("shows collection health next to the history", async () => {
    vi.mocked(api.listCustomMetricNodes).mockResolvedValue(["n1"])
    vi.mocked(api.getCustomMetricHealth).mockResolvedValue({
      metric: { id: "m1", name: "http_latency", enabled: true, interval_seconds: 30, stale_after_seconds: 90 },
      nodes: [
        { node_id: "n1", state: "ok", value: 12.5, age_seconds: 12, cadence_ms: 30000,
          definition_version: "v1", sample_id: "abcdefgh-1111-4111-8111-111111111111" },
        { node_id: "n2", state: "missing", age_seconds: 0 },
      ],
    })
    vi.mocked(api.getNodeMetrics).mockResolvedValue({})
    view()
    fireEvent.click(await screen.findByRole("button", { name: "View data" }))
    expect(await screen.findByText("Collection health")).toBeInTheDocument()
    expect(await screen.findByText("Collecting")).toBeInTheDocument()
    expect(screen.getByText("No data")).toBeInTheDocument()
    expect(screen.getByText("Primary")).toBeInTheDocument()
    expect(screen.getByText("n2")).toBeInTheDocument()
    expect(screen.getByText(/Last sample 12s/)).toBeInTheDocument()
    expect(screen.getByText(/No measurement received/)).toBeInTheDocument()
  })

  it("keeps the history rendered when collection health is unavailable", async () => {
    vi.mocked(api.listCustomMetricNodes).mockResolvedValue(["n1"])
    vi.mocked(api.getCustomMetricHealth).mockRejectedValue(new Error("503"))
    vi.mocked(api.getNodeMetrics).mockResolvedValue({
      "custom_http_latency": [{ timestamp: 1700000000, value: 12.5 }],
    })
    view()
    fireEvent.click(await screen.findByRole("button", { name: "View data" }))
    expect(await screen.findByTestId("metrics-chart")).toBeInTheDocument()
    expect(screen.queryByText("Collection health")).not.toBeInTheDocument()
  })

  it("views collected data", async () => {
    vi.mocked(api.listCustomMetricNodes).mockResolvedValue(["n1"])
    vi.mocked(api.getNodeMetrics).mockResolvedValue({
      "custom_http_latency": [{ timestamp: 1700000000, value: 12.5 }],
    })
    view()
    fireEvent.click(await screen.findByRole("button", { name: "View data" }))
    expect(await screen.findByTestId("metrics-chart")).toBeInTheDocument()
    await waitFor(() => expect(api.getNodeMetrics).toHaveBeenCalled())
  })
})
