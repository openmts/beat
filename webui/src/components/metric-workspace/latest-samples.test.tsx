import { render, screen } from "@testing-library/react"
import MockAdapter from "axios-mock-adapter"
import { afterEach, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { api } from "@/lib/api-client"
import { WorkspaceLatestSamples, latestSampleRequests } from "./latest-samples"
import { workspaceResult } from "@/test/metric-workspace-fixtures"

const mock = new MockAdapter(api)
afterEach(() => mock.reset())

function draw(publicWorkspace = false) {
  render(<LocaleProvider><WorkspaceLatestSamples series={workspaceResult().series} locale="en" publicWorkspace={publicWorkspace} /></LocaleProvider>)
}

it("requests one latest sample per node and metric and renders the raw value", async () => {
  mock.onPost("/admin/metrics/query/latest").reply(200, { code: 200, data: { series: [{ ref_id: "A", node_id: "node-one", metric: "cpu", label: "CPU usage", unit: "percent", state: "ok", value: 41.5, sample_at: "2026-10-17T02:00:00.000Z" }] } })
  draw()
  expect(await screen.findByText("41.5%")).toBeTruthy()
  expect(JSON.parse(mock.history.post[0].data)).toEqual({ series: [{ ref_id: "A", node_id: "node-one", metric: "cpu" }] })
  expect(screen.getByText("Latest samples · raw newest sample")).toBeTruthy()
  // 采样时间必须带时分秒：只给日期无法判断样本新鲜度。
  expect(screen.getByText(/\d{1,2}:\d{2}:\d{2}/)).toBeTruthy()
})

it("uses the public endpoint and drops device series that the instant query cannot address", async () => {
  const series = [...workspaceResult().series, { ...workspaceResult().series[0], ref_id: "B", device_id: "a".repeat(32), metric: "gpu_utilization_percent" }]
  expect(latestSampleRequests(series).map((entry) => entry.ref_id)).toEqual(["A"])
  mock.onPost("/metrics/query/latest").reply(200, { code: 200, data: { series: [{ ref_id: "A", node_id: "node-one", metric: "cpu_usage", label: "CPU usage", unit: "percent", state: "empty" }] } })
  render(<LocaleProvider><WorkspaceLatestSamples series={series} locale="en" publicWorkspace /></LocaleProvider>)
  expect(await screen.findByText("no samples yet")).toBeTruthy()
})

it("degrades to a hint when the instant query fails", async () => {
  mock.onPost("/admin/metrics/query/latest").reply(503, { message: "unavailable" })
  draw()
  expect(await screen.findByText(/Latest samples are unavailable/)).toBeTruthy()
})

it("renders nothing when the panel produced no series", () => {
  const { container } = render(<LocaleProvider><WorkspaceLatestSamples series={[]} locale="en" /></LocaleProvider>)
  expect(container.textContent).toBe("")
})
