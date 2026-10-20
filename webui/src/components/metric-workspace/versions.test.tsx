import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { WorkspaceVersions } from "./versions"
import { listDashboardVersions } from "@/lib/metric-workspace-api"
import type { MetricDashboardVersion } from "@/types/metric-workspace"

vi.mock("@/lib/metric-workspace-api", () => ({ listDashboardVersions: vi.fn() }))
beforeEach(() => { vi.resetAllMocks() })

function version(revision: number, summary?: string): MetricDashboardVersion {
  return { dashboard_id: "board-1", revision, summary, created_at: "2026-10-16T09:30:00Z", created_by: "preview" }
}

function view(props: Partial<Parameters<typeof WorkspaceVersions>[0]> = {}) {
  return { dashboardId: "board-1", currentRevision: 3, refreshKey: 0, restore: vi.fn(), close: vi.fn(), onError: vi.fn(), ...props }
}

it("marks the current revision and restores an older one with the note", async () => {
  const user = userEvent.setup()
  vi.mocked(listDashboardVersions).mockResolvedValue([version(3, "save-2"), version(2)])
  const props = view()
  render(<LocaleProvider><WorkspaceVersions {...props} /></LocaleProvider>)
  const rows = await screen.findAllByRole("listitem")
  expect(rows).toHaveLength(2)
  expect(within(rows[0]).getByText("Current")).toBeInTheDocument()
  expect(within(rows[0]).getByRole("button", { name: "Restore" })).toBeDisabled()
  expect(within(rows[0]).getByText(/save-2/)).toBeInTheDocument()
  expect(within(rows[1]).getByRole("button", { name: "Restore" })).toBeEnabled()
  await user.type(screen.getByLabelText("Restore note"), "rollback")
  await user.click(within(rows[1]).getByRole("button", { name: "Restore" }))
  expect(props.restore).toHaveBeenCalledWith(2, "rollback")
})

it("keeps the restore note within the 200 byte server limit", async () => {
  const user = userEvent.setup()
  vi.mocked(listDashboardVersions).mockResolvedValue([version(3)])
  render(<LocaleProvider><WorkspaceVersions {...view({ currentRevision: 9 })} /></LocaleProvider>)
  const field = await screen.findByLabelText("Restore note")
  await user.click(field)
  await user.paste("汉".repeat(90))
  // 每个汉字 3 字节：90 字会超限，输入被截断到不超过 200 字节。
  expect(new TextEncoder().encode(field.getAttribute("value") ?? (field as HTMLInputElement).value).length).toBeLessThanOrEqual(200)
  expect((field as HTMLInputElement).value.length).toBeLessThan(90)
})

it("reports an empty history and load failures through the caller", async () => {
  vi.mocked(listDashboardVersions).mockResolvedValueOnce([])
  const empty = view()
  const first = render(<LocaleProvider><WorkspaceVersions {...empty} /></LocaleProvider>)
  expect(await screen.findByText("This dashboard has no saved revisions yet.")).toBeInTheDocument()
  expect(empty.onError).not.toHaveBeenCalled()
  first.unmount()
  vi.mocked(listDashboardVersions).mockRejectedValueOnce(new Error("boom"))
  const failed = view()
  render(<LocaleProvider><WorkspaceVersions {...failed} /></LocaleProvider>)
  await vi.waitFor(() => expect(failed.onError).toHaveBeenCalledWith("workspace.load_failed"))
})

it("closes through the cancel button and the dialog", async () => {
  const user = userEvent.setup()
  vi.mocked(listDashboardVersions).mockResolvedValue([version(1)])
  const props = view()
  render(<LocaleProvider><WorkspaceVersions {...props} /></LocaleProvider>)
  await user.click(await screen.findByRole("button", { name: "Cancel" }))
  expect(props.close).toHaveBeenCalledOnce()
  await user.keyboard("{Escape}")
  expect(props.close).toHaveBeenCalledTimes(2)
})
