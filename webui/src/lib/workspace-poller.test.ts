import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { queryWorkspaceMetrics } from "@/lib/metric-workspace-api"
import { APIError } from "@/lib/api-error"
import { WorkspacePoller, type WorkspacePollEvent, type WorkspacePollingOptions } from "@/lib/workspace-poller"
import { workspaceDashboard, workspaceNode, workspaceResult } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/metric-workspace-api", () => ({ queryWorkspaceMetrics: vi.fn() }))
beforeEach(() => { vi.useFakeTimers({ now: new Date("2026-09-13T01:00:00Z") }); vi.mocked(queryWorkspaceMetrics).mockReset().mockResolvedValue(workspaceResult()) })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

function options(): WorkspacePollingOptions {
  return { dashboardId: "test", definition: workspaceDashboard().definition, nodes: [workspaceNode()], groups: [], selections: { node: ["node-one"] }, range: { mode: "relative", seconds: 3600 } }
}

it.each(["absolute", "relative"] as const)("loads a hidden %s workspace once visible even without refresh", async (mode) => {
  const visibility = vi.spyOn(document, "hidden", "get").mockReturnValue(true)
  const input = options()
  input.definition.refresh_seconds = 0
  if (mode === "absolute") input.range = { mode, from: "2026-09-13T00:00Z", to: "2026-09-13T01:00Z" }
  const stop = new WorkspacePoller(input, () => undefined).start()
  expect(queryWorkspaceMetrics).not.toHaveBeenCalled()
  visibility.mockReturnValue(false)
  document.dispatchEvent(new Event("visibilitychange"))
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  visibility.mockReturnValue(true)
  document.dispatchEvent(new Event("visibilitychange"))
  visibility.mockReturnValue(false)
  document.dispatchEvent(new Event("visibilitychange"))
  await vi.advanceTimersByTimeAsync(60000)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  stop()
})

it("never applies a canceled response or schedules work after unmount", async () => {
  const events: WorkspacePollEvent[] = []
  let resolve!: (value: ReturnType<typeof workspaceResult>) => void
  vi.mocked(queryWorkspaceMetrics).mockImplementation(() => new Promise((done) => { resolve = done }))
  const input = options()
  input.definition.refresh_seconds = 5
  const stop = new WorkspacePoller(input, (event) => events.push(event)).start()
  const signal = vi.mocked(queryWorkspaceMetrics).mock.calls[0][1].signal
  stop()
  resolve(workspaceResult())
  await vi.advanceTimersByTimeAsync(10000)
  expect(signal.aborted).toBe(true)
  expect(events.some((event) => event.type === "panel" && event.patch.result)).toBe(false)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
})

it("resumes an interrupted historical query without publishing its canceled completion", async () => {
  const visibility = vi.spyOn(document, "hidden", "get").mockReturnValue(false)
  const events: WorkspacePollEvent[] = []
  let resolve!: (value: ReturnType<typeof workspaceResult>) => void
  vi.mocked(queryWorkspaceMetrics).mockImplementationOnce(() => new Promise((done) => { resolve = done }))
  const input = options()
  input.range = { mode: "absolute", from: "2026-09-13T00:00Z", to: "2026-09-13T01:00Z" }
  const stop = new WorkspacePoller(input, (event) => events.push(event)).start()
  visibility.mockReturnValue(true)
  document.dispatchEvent(new Event("visibilitychange"))
  resolve(workspaceResult())
  await vi.advanceTimersByTimeAsync(0)
  expect(events.some((event) => event.type === "complete")).toBe(false)
  visibility.mockReturnValue(false)
  document.dispatchEvent(new Event("visibilitychange"))
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
  expect(events.filter((event) => event.type === "complete")).toHaveLength(1)
  stop()
})

it("honors Retry-After and keeps the failed request distinct from the last result", async () => {
  const events: WorkspacePollEvent[] = []
  const input = options()
  input.definition.refresh_seconds = 5
  vi.mocked(queryWorkspaceMetrics).mockRejectedValueOnce(new APIError("busy", 429, "30"))
  const stop = new WorkspacePoller(input, (event) => events.push(event)).start()
  await vi.advanceTimersByTimeAsync(29999)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  expect(events.some((event) => event.type === "panel" && event.patch.error === "busy")).toBe(true)
  await vi.advanceTimersByTimeAsync(1)
  expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
  expect(events.some((event) => event.type === "panel" && event.patch.resultQuery && event.patch.resultExpanded)).toBe(true)
  stop()
})

it("pauses in hidden tabs and resumes live ranges", async () => {
  const visibility = vi.spyOn(document, "hidden", "get").mockReturnValue(false)
  const input = options()
  input.definition.refresh_seconds = 5
  const stop = new WorkspacePoller(input, () => undefined).start()
  await vi.advanceTimersByTimeAsync(0)
  visibility.mockReturnValue(true)
  document.dispatchEvent(new Event("visibilitychange"))
  await vi.advanceTimersByTimeAsync(30000)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  visibility.mockReturnValue(false)
  document.dispatchEvent(new Event("visibilitychange"))
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
  stop()
})

it("queries a panel with its own relative time override", async () => {
  const input = options()
  input.definition.panels[0].time_from = "now-30m"
  const stop = new WorkspacePoller(input, () => undefined).start()
  await vi.advanceTimersByTimeAsync(0)
  const query = vi.mocked(queryWorkspaceMetrics).mock.calls[0][0]
  expect(query.from).toBe("2026-09-13T00:30:00.000Z")
  expect(query.to).toBe("2026-09-13T01:00:00.000Z")
  stop()
})

it("applies the dashboard now delay to relative ranges and the resolved point budget", async () => {
  const input = options()
  input.definition.now_delay_seconds = 60
  input.definition.max_points = 300
  input.definition.panels[0].max_points = 90
  const stop = new WorkspacePoller(input, () => undefined).start()
  await vi.advanceTimersByTimeAsync(0)
  const query = vi.mocked(queryWorkspaceMetrics).mock.calls[0][0]
  expect(query.to).toBe("2026-09-13T00:59:00.000Z")
  expect(query.from).toBe("2026-09-12T23:59:00.000Z")
  expect(query.max_points).toBe(90)
  stop()
  input.definition.panels[0].max_points = undefined
  const next = new WorkspacePoller(input, () => undefined).start()
  await vi.advanceTimersByTimeAsync(0)
  expect(vi.mocked(queryWorkspaceMetrics).mock.calls[1][0].max_points).toBe(300)
  next()
})

it("shifts a panel window back by its declared time shift", async () => {
  const input = options()
  input.definition.panels[0].time_shift = "1d"
  const stop = new WorkspacePoller(input, () => undefined).start()
  await vi.advanceTimersByTimeAsync(0)
  const query = vi.mocked(queryWorkspaceMetrics).mock.calls[0][0]
  expect(query.to).toBe("2026-09-12T01:00:00.000Z")
  expect(query.from).toBe("2026-09-12T00:00:00.000Z")
  stop()
})

it("holds historical ranges fixed and reports missing selections without requests", async () => {
  const input = options()
  input.range = { mode: "absolute", from: "2026-09-13T00:00Z", to: "2026-09-13T01:00Z" }
  input.definition.refresh_seconds = 5
  const stop = new WorkspacePoller(input, () => undefined).start()
  await vi.advanceTimersByTimeAsync(60000)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  stop()
  input.selections = {}
  const events: WorkspacePollEvent[] = []
  const end = new WorkspacePoller(input, (event) => events.push(event)).start()
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  expect(events.some((event) => event.type === "panel" && event.patch.error === "workspace.select_node")).toBe(true)
  end()
})

it("re-queries a single panel on demand and ignores unknown panels", async () => {
  const events: WorkspacePollEvent[] = []
  const input = options()
  const poller = new WorkspacePoller(input, (event) => events.push(event))
  const stop = poller.start()
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledOnce()
  poller.retry("cpu")
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
  expect(events.filter((event) => event.type === "panel" && event.id === "cpu" && event.patch.result)).toHaveLength(2)
  poller.retry("missing-panel")
  await vi.advanceTimersByTimeAsync(0)
  expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(2)
  stop()
})

it("aborts the superseded retry of the same panel and drops its canceled result", async () => {
  const events: WorkspacePollEvent[] = []
  const pending: Array<(value: ReturnType<typeof workspaceResult>) => void> = []
  const input = options()
  vi.mocked(queryWorkspaceMetrics).mockResolvedValueOnce(workspaceResult())
    .mockImplementation(() => new Promise((done) => { pending.push(done) }))
  const poller = new WorkspacePoller(input, (event) => events.push(event))
  const stop = poller.start()
  await vi.advanceTimersByTimeAsync(0)
  poller.retry("cpu")
  await vi.advanceTimersByTimeAsync(0)
  const superseded = vi.mocked(queryWorkspaceMetrics).mock.calls.at(-1)![1].signal
  poller.retry("cpu")
  await vi.advanceTimersByTimeAsync(0)
  expect(superseded.aborted).toBe(true)
  expect(queryWorkspaceMetrics).toHaveBeenCalledTimes(3)
  for (const resolve of pending) resolve(workspaceResult())
  await vi.advanceTimersByTimeAsync(0)
  expect(events.filter((event) => event.type === "panel" && event.patch.result)).toHaveLength(2)
  stop()
})

it("aborts an in-flight panel retry when the poller stops", async () => {
  const events: WorkspacePollEvent[] = []
  const pending: Array<(value: ReturnType<typeof workspaceResult>) => void> = []
  const input = options()
  vi.mocked(queryWorkspaceMetrics).mockResolvedValueOnce(workspaceResult())
    .mockImplementation(() => new Promise((done) => { pending.push(done) }))
  const poller = new WorkspacePoller(input, (event) => events.push(event))
  const stop = poller.start()
  await vi.advanceTimersByTimeAsync(0)
  poller.retry("cpu")
  await vi.advanceTimersByTimeAsync(0)
  stop()
  for (const resolve of pending) resolve(workspaceResult())
  await vi.advanceTimersByTimeAsync(0)
  expect(events.filter((event) => event.type === "panel" && event.patch.result)).toHaveLength(1)
})
