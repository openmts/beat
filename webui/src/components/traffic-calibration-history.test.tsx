import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { TrafficCalibrationHistory } from "@/components/traffic-calibration-history"
import { LocaleProvider } from "@/context/locale"
import * as calibrationApi from "@/lib/traffic-calibration"
import type { TrafficCalibrationEvent } from "@/lib/traffic-calibration"

vi.mock("@/lib/traffic-calibration", async () => {
  const actual = await vi.importActual<typeof import("@/lib/traffic-calibration")>("@/lib/traffic-calibration")
  return { ...actual, listTrafficCalibrations: vi.fn() }
})

vi.mock("@/components/ui/sheet", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Sheet: mocks.MockDialog,
    SheetContent: mocks.PassThrough,
    SheetHeader: mocks.PassThrough,
    SheetTitle: mocks.PassThrough,
    SheetDescription: mocks.PassThrough,
  }
})

const listMock = vi.mocked(calibrationApi.listTrafficCalibrations)

function event(overrides: Partial<TrafficCalibrationEvent> = {}): TrafficCalibrationEvent {
  return {
    id: "event-1",
    node_id: "node-1",
    node_name: "node",
    kind: "set",
    effective_at: "2026-10-01T00:00:00Z",
    target_sent: "1024",
    target_received: "2000",
    reason: "opening balance",
    actor: { id: "user", name: "owner", session_id: "session", request_id: "req" },
    created_at: "2026-10-01T00:01:00Z",
    ...overrides,
  }
}

function renderHistory(overrides: Partial<Parameters<typeof TrafficCalibrationHistory>[0]> = {}) {
  const onRevoke = vi.fn()
  const onOpenChange = vi.fn()
  render(
    <LocaleProvider>
      <TrafficCalibrationHistory nodeId="node-1" open pending={false} onOpenChange={onOpenChange} onRevoke={onRevoke} {...overrides} />
    </LocaleProvider>,
  )
  return { onRevoke, onOpenChange }
}

describe("TrafficCalibrationHistory", () => {
  beforeEach(() => {
    listMock.mockReset()
  })

  it("shows an empty state when the ledger has no events", async () => {
    listMock.mockResolvedValue({ events: [] })
    renderHistory()
    expect(await screen.findByText(/No calibration events yet|暂无校准事件/i)).toBeDefined()
  })

  it("lists events with byte targets and revokes only the active set", async () => {
    listMock.mockResolvedValue({
      events: [event(), event({ id: "event-2", kind: "revoke", effective_at: undefined, target_sent: undefined, target_received: undefined })],
    })
    const { onRevoke } = renderHistory()
    expect(await screen.findByText("1 KiB")).toBeDefined()
    expect(screen.getByText("1.9 KiB")).toBeDefined()
    const revokeButtons = screen.getAllByRole("button", { name: /Revoke|撤销/i })
    expect(revokeButtons).toHaveLength(1)
    fireEvent.click(revokeButtons[0]!)
    expect(onRevoke).toHaveBeenCalledWith("event-1")
  })

  it("paginates older events with the returned cursor", async () => {
    listMock.mockResolvedValueOnce({ events: [event()], next_cursor: "cursor-2" })
    listMock.mockResolvedValueOnce({ events: [event({ id: "event-older" })] })
    renderHistory()
    const more = await screen.findByRole("button", { name: /Load older events|加载更早的事件/i })
    fireEvent.click(more)
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2))
    expect(listMock).toHaveBeenLastCalledWith("node-1", { limit: 25, cursor: "cursor-2" })
  })

  it("surfaces load failures without hiding the sheet", async () => {
    listMock.mockRejectedValue(new Error("calibration failed"))
    renderHistory()
    expect(await screen.findByText("calibration failed")).toBeDefined()
  })

  it("does not load while closed or without a node", () => {
    renderHistory({ open: false })
    renderHistory({ nodeId: null })
    expect(listMock).not.toHaveBeenCalled()
  })
})
