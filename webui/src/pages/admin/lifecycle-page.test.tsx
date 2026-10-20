import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import LifecyclePage from "./lifecycle"
import * as api from "@/lib/lifecycle-api"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/hooks/use-api", () => ({ useNodes: () => ({ data: [workspaceNode("one"), workspaceNode("two")], loading: false, error: null }) }))
vi.mock("@/lib/lifecycle-api", () => ({ getGeoIPSettings: vi.fn(), getLifecycleSettings: vi.fn(), getLocation: vi.fn(), getLifecycle: vi.fn(), getLifecycleEvents: vi.fn() }))
vi.mock("./geoip-settings-editor", () => ({ GeoIPSettingsEditor: ({ saved }: { saved: () => void }) => <button onClick={saved}>Save geo</button> }))
vi.mock("./lifecycle-settings-editor", () => ({ LifecycleSettingsEditor: () => <p>Global lifecycle editor</p> }))
vi.mock("./node-location-editor", () => ({ NodeLocationEditor: ({ value }: { value: { node_id: string } }) => <p>Location {value.node_id}</p> }))
vi.mock("./node-lifecycle-editor", () => ({ NodeLifecycleEditor: () => <p>Node lifecycle editor</p> }))
beforeEach(() => { vi.resetAllMocks(); auth.principal.user.role = "owner"; vi.mocked(api.getGeoIPSettings).mockResolvedValue({} as never); vi.mocked(api.getLifecycleSettings).mockResolvedValue({} as never); vi.mocked(api.getLocation).mockImplementation(async id => ({ node_id: id }) as never); vi.mocked(api.getLifecycle).mockResolvedValue({} as never); vi.mocked(api.getLifecycleEvents).mockResolvedValue([]) })
describe("lifecycle page integration", () => {
 it("loads owner defaults, selected node and bounded history", async () => {
  vi.mocked(api.getLifecycleEvents).mockResolvedValue([{ id: "event", created_at: "2026-10-01", kind: "advanced", old_expires_on: "2026-10-01", new_expires_on: "2026-11-01", actor: "owner", routing_status: "queued", skipped_intervals: 0 }] as never)
  render(<LifecyclePage />); expect(await screen.findByText("Global lifecycle editor")).toBeInTheDocument(); fireEvent.change(screen.getByLabelText("Node"), { target: { value: "one" } }); expect(await screen.findByText("Location one")).toBeInTheDocument(); expect(screen.getByText(/advanced/)).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Save geo" })); await waitFor(() => expect(api.getGeoIPSettings).toHaveBeenCalledTimes(2))
 })
 it("hides global controls from admin and ignores abandoned node requests", async () => {
  auth.principal.user.role = "admin"; let finish!: (value: never) => void; vi.mocked(api.getLocation).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  render(<LifecyclePage />); expect(api.getGeoIPSettings).not.toHaveBeenCalled(); fireEvent.change(screen.getByLabelText("Node"), { target: { value: "one" } }); fireEvent.change(screen.getByLabelText("Node"), { target: { value: "two" } }); expect(await screen.findByText("Location two")).toBeInTheDocument(); finish({ node_id: "one" } as never); await Promise.resolve(); expect(screen.queryByText("Location one")).not.toBeInTheDocument(); expect(screen.getByText("No events")).toBeInTheDocument()
 })
 it("surfaces and retries defaults and node errors", async () => {
  vi.mocked(api.getGeoIPSettings).mockRejectedValueOnce(new Error("defaults unavailable")); render(<LifecyclePage />); expect(await screen.findByText("defaults unavailable")).toBeInTheDocument(); await userEvent.click(screen.getByRole("button", { name: "Retry" })); expect(await screen.findByText("Global lifecycle editor")).toBeInTheDocument()
  vi.mocked(api.getLocation).mockRejectedValueOnce(new Error("node unavailable")); fireEvent.change(screen.getByLabelText("Node"), { target: { value: "one" } }); expect(await screen.findByText("node unavailable")).toBeInTheDocument(); await userEvent.click(screen.getByRole("button", { name: "Retry" })); expect(await screen.findByText("Location one")).toBeInTheDocument()
 })
})
