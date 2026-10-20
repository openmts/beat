import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import * as runtimeApi from "@/lib/runtime-api"
import type { RuntimeStatus } from "@/types/runtime"
import { OperationRuntime, isRuntimeStatus } from "./operation-runtime"

vi.mock("@/lib/runtime-api")

const worker = (overrides: Partial<RuntimeStatus["workers"][number]> = {}) => ({
  name: "alerts",
  configured: true,
  started: true,
  running: true,
  state: "healthy",
  failures: 0,
  consecutive_failures: 0,
  restarts: 0,
  interval_seconds: 60,
  scheduler_lag_seconds: 0.5,
  started_at: "2026-10-14T00:00:00Z",
  stopped_at: "",
  last_attempt: "2026-10-14T00:00:00Z",
  last_success: "2026-10-14T00:00:00Z",
  last_failure: "",
  ...overrides,
})
const status = (overrides: Partial<RuntimeStatus> = {}): RuntimeStatus => ({
  server_time: "2026-10-14T00:00:00Z",
  stopping: false,
  workers: [worker()],
  metrics: {
    generation: 7,
    timestamp: "2026-10-14T00:00:00Z",
    age_seconds: 1,
    stale: false,
    available: true,
    connections: 2,
    reserved: 0,
    tracked_ips: 3,
    builds: 10,
    failures: 0,
    dropped: 0,
    build_duration_seconds: 0.02,
    skipped_cadences: 0,
    closed: false,
  },
  ...overrides,
})

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(runtimeApi.getRuntimeStatus).mockResolvedValue(status())
})

it("renders localized worker rows without raw identifiers", async () => {
  render(<LocaleProvider><OperationRuntime /></LocaleProvider>)
  expect(await screen.findByText("Alert evaluation")).toBeInTheDocument()
  expect(screen.getByText("Healthy")).toBeInTheDocument()
  expect(screen.queryByText("alerts")).not.toBeInTheDocument()
})

it("summarizes degradation reasons without start or stop controls", async () => {
  vi.mocked(runtimeApi.getRuntimeStatus).mockResolvedValue(status({
    stopping: true,
    workers: [
      worker({ configured: true, running: false, state: "stopped" }),
      worker({ name: "geoip", state: "degraded", consecutive_failures: 2 }),
      worker({ name: "accelerators", state: "starting" }),
    ],
    metrics: { ...status().metrics, available: false, closed: true },
  }))
  render(<LocaleProvider><OperationRuntime /></LocaleProvider>)
  expect(await screen.findByText("Runtime degraded")).toBeInTheDocument()
  expect(screen.queryByRole("button", { name: /stop|restart/i })).not.toBeInTheDocument()
})

it("reports an unavailable status when loading fails", async () => {
  vi.mocked(runtimeApi.getRuntimeStatus).mockRejectedValue(new Error("offline"))
  render(<LocaleProvider><OperationRuntime /></LocaleProvider>)
  expect(await screen.findByText(/Runtime status is unavailable/)).toBeInTheDocument()
})

it("rejects payloads that do not match the runtime contract", () => {
  expect(isRuntimeStatus(undefined)).toBe(false)
  expect(isRuntimeStatus({ workers: [], metrics: {} })).toBe(false)
  expect(isRuntimeStatus(status())).toBe(true)
})

it("shows worker aging, lag fallback and stream details", async () => {
  const user = userEvent.setup(), now = Date.now()
  vi.mocked(runtimeApi.getRuntimeStatus).mockResolvedValue(status({
    workers: [
      worker({ name: "alerts", last_success: new Date(now - 5 * 60 * 1000).toISOString() }),
      worker({ name: "maintenance", interval_seconds: 0, last_success: new Date(now - 3 * 3600 * 1000).toISOString(), last_failure: "not-a-date" }),
      worker({ name: "geoip", last_success: "" }),
    ],
  }))
  render(<LocaleProvider><OperationRuntime /></LocaleProvider>)
  expect(await screen.findByText("5m ago")).toBeInTheDocument()
  expect(screen.getByText("3h ago")).toBeInTheDocument()
  expect(screen.getByText("Maintenance and backups")).toBeInTheDocument()
  expect(screen.getAllByText("—").length).toBeGreaterThan(0)
  await user.click(screen.getByRole("tab", { name: "Metrics stream" }))
  expect(await screen.findByText("Available")).toBeInTheDocument()
  expect(screen.getByText("Generation")).toBeInTheDocument()
})
