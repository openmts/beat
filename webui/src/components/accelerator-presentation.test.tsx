import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { AcceleratorSummary } from "./accelerator-summary"
import { AcceleratorCurrent } from "./accelerator-current"
import { AcceleratorDetail } from "./accelerator-detail"
import { acceleratorCatalog, acceleratorDevice } from "@/test/accelerator-fixtures"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import * as metrics from "@/lib/metric-workspace-api"
import type { MetricQuery } from "@/types/metric-workspace"
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/lib/metric-workspace-api", () => ({ queryWorkspaceMetrics: vi.fn(), queryWorkspaceLatest: vi.fn(async () => ({ series: [] })) }))
vi.mock("./metric-workspace/select", () => ({ WorkspaceSelect: ({ label, value, options, onChange }: { label: string; value: string; options: { value: string; label: string }[]; onChange: (value: string) => void }) => <select aria-label={label} value={value} onChange={event => onChange(event.target.value)}>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select> }))
vi.mock("./metric-workspace/panel", () => ({ WorkspacePanel: ({ panel, state }: { panel: { title: string }; state?: { result?: { from: string; to: string }; error?: string } }) => <section aria-label={panel.title} data-from={state?.result?.from} data-to={state?.result?.to}>{state?.error || panel.title}</section> }))
beforeEach(() => { vi.clearAllMocks(); vi.mocked(metrics.queryWorkspaceMetrics).mockImplementation(async (query: MetricQuery) => ({ ...query, interval_ms: 1000, series: query.series.map(series => ({ ...series, label: series.metric, unit: series.metric.includes("bytes") ? "bytes" : "percent", available_from: null, state: "empty", points: [] })) })) })
describe("GPU public presentation", () => {
 it("handles zero, one and multiple devices without zero filling missing metrics", () => {
  const view = render(<AcceleratorSummary devices={[]} />); expect(view.container).toBeEmptyDOMElement()
  view.rerender(<AcceleratorSummary devices={[acceleratorDevice()]} />); expect(screen.getByText("GPU 1 · NVIDIA Test GPU")).toBeInTheDocument(); expect(screen.getByText("25.0%")).toBeInTheDocument(); expect(screen.getByText("1 GiB / 2 GiB")).toBeInTheDocument()
  view.rerender(<AcceleratorSummary devices={[acceleratorDevice(), { ...acceleratorDevice("b".repeat(32)), ordinal: 2, stale: true, model: "Long model ".repeat(30), metrics: { gpu_utilization_percent: 90 } }]} />); expect(screen.getByText("2 GPUs")).toBeInTheDocument(); expect(screen.getByText("Peak 90.0%")).toBeInTheDocument(); expect(screen.getByText("Stale or missing")).toBeInTheDocument(); expect(screen.getByText("—")).toBeInTheDocument()
 })
 it("formats dynamic units and explicit unsupported, missing, partial and stale states", () => {
  const device = { ...acceleratorDevice(), stale: true, metrics: { gpu_utilization_percent: 0 }, capabilities: ["gpu_utilization_percent", "gpu_temperature_celsius"] }
  render(<AcceleratorCurrent device={device} catalog={acceleratorCatalog()} />); expect(screen.getByText("0%")).toBeInTheDocument(); expect(screen.getByText("Stale samples")).toBeInTheDocument(); expect(screen.getByText("Some metrics are unavailable")).toBeInTheDocument(); expect(screen.getAllByText("Metric unavailable")).toHaveLength(4)
 })
 it("selects stable device aliases and binds history queries and domains to the requested range", async () => {
  const first = acceleratorDevice(), second = { ...acceleratorDevice("b".repeat(32)), ordinal: 2, model: "AMD GPU" }, node = { ...workspaceNode(), accelerators: [first, second] }
  const props = { node, catalog: acceleratorCatalog(), range: { mode: "absolute" as const, from: "2026-10-07T00:00:00Z", to: "2026-10-07T01:00:00Z" }, refreshSeconds: 0, onRange: vi.fn(), onInterval: vi.fn() }
  const view = render(<AcceleratorDetail {...props} />); expect(metrics.queryWorkspaceMetrics).not.toHaveBeenCalled(); expect(screen.getByLabelText("GPU device")).toHaveDisplayValue("GPU 1 · NVIDIA Test GPU")
  await userEvent.click(screen.getByRole("tab", { name: "Device history" })); await waitFor(() => expect(metrics.queryWorkspaceMetrics).toHaveBeenCalledTimes(4))
  expect(vi.mocked(metrics.queryWorkspaceMetrics).mock.calls[0][0]).toMatchObject({ from: "2026-10-07T00:00:00.000Z", to: "2026-10-07T01:00:00.000Z", max_points: 600, series: [expect.objectContaining({ device_id: first.device_id })] })
  fireEvent.change(screen.getByLabelText("GPU device"), { target: { value: second.device_id } }); await waitFor(() => expect(metrics.queryWorkspaceMetrics).toHaveBeenCalledTimes(8)); expect(vi.mocked(metrics.queryWorkspaceMetrics).mock.calls.at(-1)![0].series[0].device_id).toBe(second.device_id)
  view.rerender(<AcceleratorDetail {...props} range={{ mode: "absolute", from: "2026-10-06T00:00:00Z", to: "2026-10-07T00:00:00Z" }} />); await waitFor(() => expect(metrics.queryWorkspaceMetrics).toHaveBeenCalledTimes(12)); expect(screen.getByRole("region", { name: "GPU utilization" })).toHaveAttribute("data-from", "2026-10-06T00:00:00.000Z")
  view.rerender(<AcceleratorDetail {...props} node={{ ...node, accelerators: [first] }} />); expect(screen.getByLabelText("GPU device")).toHaveDisplayValue("Device inactive or removed"); expect(screen.queryByRole("tab", { name: "Device history" })).not.toBeInTheDocument()
 })
 it("keeps absent or private GPU inventory neutral", () => { render(<AcceleratorDetail node={workspaceNode()} catalog={[]} range={{ mode: "relative", seconds: 3600 }} refreshSeconds={0} onRange={vi.fn()} onInterval={vi.fn()} />); expect(screen.getByText("GPU devices are not public or no devices are available.")).toBeInTheDocument(); expect(metrics.queryWorkspaceMetrics).not.toHaveBeenCalled() })
})
