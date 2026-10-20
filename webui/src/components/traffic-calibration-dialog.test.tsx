import { Children, isValidElement, type ReactNode } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { TrafficCalibrationDialog, buildDraft } from "@/components/traffic-calibration-dialog"
import { LocaleProvider } from "@/context/locale"
import type { ManagedNode } from "@/types"

vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog,
    DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough,
    DialogTitle: mocks.PassThrough,
    DialogDescription: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
  }
})

vi.mock("@/components/ui/alert-dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    AlertDialog: mocks.MockDialog,
    AlertDialogContent: mocks.PassThrough,
    AlertDialogHeader: mocks.PassThrough,
    AlertDialogTitle: mocks.PassThrough,
    AlertDialogDescription: mocks.PassThrough,
    AlertDialogFooter: mocks.PassThrough,
    AlertDialogCancel: ({ children, ...rest }: { children: ReactNode }) => <button {...rest}>{children}</button>,
    AlertDialogAction: ({ children, ...rest }: { children: ReactNode }) => <button {...rest}>{children}</button>,
  }
})

vi.mock("@/components/ui/toggle-group", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    ToggleGroup: ({ children, value, onValueChange }: {
      children: ReactNode
      value: string[]
      onValueChange: (value: string[]) => void
    }) => {
      const items: ReactNode[] = []
      Children.forEach(children, (child) => {
        if (!isValidElement<{ value?: string; children?: ReactNode; "aria-label"?: string }>(child)) return
        items.push(
          <button
            key={child.props.value}
            aria-pressed={child.props.value === value[0]}
            aria-label={child.props["aria-label"]}
            onClick={() => onValueChange([String(child.props.value)])}
          >
            {child.props.children}
          </button>,
        )
      })
      return <div data-testid="toggle-group">{items}</div>
    },
    ToggleGroupItem: mocks.PassThrough,
  }
})

const node = {
  id: "node-1",
  name: "edge",
  alias: "",
  host: "192.0.2.1",
  port: 22,
  traffic: { sent: 1024, received: 2048, used: 3072, limit: 0, remaining: null, percentage: null, limit_type: "sum", reset_day: 1, period_start: "2026-10-01T00:00:00Z", next_reset: "2026-11-01T00:00:00Z", tracked_since: null, status: "unlimited" },
} as unknown as ManagedNode

function renderDialog(overrides: Partial<Parameters<typeof TrafficCalibrationDialog>[0]> = {}) {
  const onSubmit = vi.fn()
  const onOpenChange = vi.fn()
  const onOpenHistory = vi.fn()
  render(
    <LocaleProvider>
      <TrafficCalibrationDialog
        node={node}
        open
        pending={false}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
        onOpenHistory={onOpenHistory}
        {...overrides}
      />
    </LocaleProvider>,
  )
  return { onSubmit, onOpenChange, onOpenHistory }
}

describe("buildDraft", () => {
  it("converts binary unit input into canonical byte strings", () => {
    const draft = buildDraft({ mode: "set", upload: "1.5 GiB", download: "512 MiB", reason: "correction", effective: "" })
    expect(draft.code).toBe("")
    expect(draft.payload.target_sent).toBe(String(Math.round(1.5 * 1024 ** 3)))
    expect(draft.payload.target_received).toBe(String(512 * 1024 ** 2))
  })

  it("forces zero targets in reset mode", () => {
    const draft = buildDraft({ mode: "reset", upload: "1 GiB", download: "1 GiB", reason: "reset", effective: "" })
    expect(draft.code).toBe("")
    expect(draft.payload.target_sent).toBe("0")
    expect(draft.payload.target_received).toBe("0")
  })

  it("flags missing reasons, malformed bytes and malformed instants", () => {
    expect(buildDraft({ mode: "set", upload: "1 GiB", download: "1 GiB", reason: "   ", effective: "" }).code).toBe("reason")
    expect(buildDraft({ mode: "set", upload: "nope", download: "1 GiB", reason: "why", effective: "" }).code).toBe("bytes")
    expect(buildDraft({ mode: "set", upload: "1 GiB", download: "1 GiB", reason: "why", effective: "not-a-date" }).code).toBe("effective")
    expect(buildDraft({ mode: "set", upload: "1 GiB", download: "1 GiB", reason: "x".repeat(501), effective: "" }).code).toBe("reason")
  })

  it("normalises explicit instants to UTC ISO strings", () => {
    const draft = buildDraft({ mode: "set", upload: "1 GiB", download: "1 GiB", reason: "why", effective: "2026-10-10T08:00" })
    expect(draft.code).toBe("")
    expect(draft.payload.effective_at).toBe(new Date("2026-10-10T08:00").toISOString())
  })
})

describe("TrafficCalibrationDialog", () => {
  it("blocks submission until the mandatory reason and byte fields are valid", () => {
    const { onSubmit } = renderDialog()
    fireEvent.click(screen.getByRole("button", { name: /Apply calibration|应用校准/i }))
    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getAllByRole("alert").length).toBeGreaterThan(0)
  })

  it("submits a set calibration after explicit confirmation", () => {
    const { onSubmit } = renderDialog()
    fireEvent.change(screen.getByLabelText(/Accounted upload|账面上行/i), { target: { value: "1 GiB" } })
    fireEvent.change(screen.getByLabelText(/Accounted download|账面下行/i), { target: { value: "2 GiB" } })
    fireEvent.change(screen.getByLabelText(/Reason|校准原因/i), { target: { value: "provider snapshot" } })
    fireEvent.click(screen.getByRole("button", { name: /Apply calibration|应用校准/i }))
    fireEvent.click(screen.getByRole("button", { name: /^Apply$|确认应用/i }))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    const [payload, mode] = onSubmit.mock.calls[0]
    expect(mode).toBe("set")
    expect(payload.target_sent).toBe(String(1024 ** 3))
    expect(payload.target_received).toBe(String(2 * 1024 ** 3))
    expect(payload.reason).toBe("provider snapshot")
  })

  it("forces zero targets when reset is selected", () => {
    const { onSubmit } = renderDialog()
    fireEvent.click(screen.getByRole("button", { name: /Reset to zero|重置为零/i }))
    expect(screen.getByText(/Reset declares zero|重置会在该生效秒/i)).toBeDefined()
    fireEvent.change(screen.getByLabelText(/Reason|校准原因/i), { target: { value: "reset the cycle" } })
    fireEvent.click(screen.getByRole("button", { name: /Apply calibration|应用校准/i }))
    fireEvent.click(screen.getByRole("button", { name: /^Apply$|确认应用/i }))
    const [payload, mode] = onSubmit.mock.calls[0]
    expect(mode).toBe("reset")
    expect(payload.target_sent).toBe("0")
    expect(payload.target_received).toBe("0")
  })

  it("opens the history sheet and closes without submitting", () => {
    const { onSubmit, onOpenHistory, onOpenChange } = renderDialog()
    fireEvent.click(screen.getByRole("button", { name: /Calibration history|校准历史/i }))
    expect(onOpenHistory).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: /Cancel|取消/i }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it("renders nothing without a selected node", () => {
    render(
      <LocaleProvider>
        <TrafficCalibrationDialog
          node={null} open pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} onOpenHistory={vi.fn()}
        />
      </LocaleProvider>,
    )
    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
