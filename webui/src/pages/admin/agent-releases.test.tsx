import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest"
import AxiosMockAdapter from "axios-mock-adapter"
import { api } from "@/lib/api-client"
import { APIError } from "@/lib/api-error"
import { agentRelease, agentRollout, agentTarget } from "@/test/agent-rollout-fixtures"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { agentRolloutError } from "@/hooks/use-agent-rollout-data"
import { testLocaleValue } from "@/test/catalogs"
import AgentReleasesPage from "./agent-releases"
import { agentObservationRemaining, canCommandAgentRollout, canDeleteAgentRollout } from "./agent-rollout-config"
import { validateAgentRolloutPolicy } from "./agent-rollout-config"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  const value = testLocaleValue("en")
  return { useLocale: () => value, useOptionalLocale: () => value }
})
const t = testLocaleValue("en").t
const mock = new AxiosMockAdapter(api)
afterAll(() => mock.restore())
const renderPage = () => render(<MemoryRouter><AgentReleasesPage /></MemoryRouter>)
beforeEach(() => {
 mock.reset(); auth.principal.user.role = "owner"; auth.principal.session.id = "session"
 mock.onGet("/admin/agent-rollout-status").reply(200, { code: 0, data: { enabled: true, configured: true, ready: true, reason: "ready" } })
 mock.onGet("/admin/agent-releases").reply(200, { code: 0, data: { items: [agentRelease()], next_cursor: "" } })
 mock.onGet("/admin/agent-rollouts").reply(200, { code: 0, data: { items: [agentRollout()], next_cursor: "" } })
 mock.onGet("/admin/agent-releases/release%2Fone").reply(200, { code: 0, data: agentRelease() })
 mock.onGet("/admin/agent-rollouts/rollout%2Fone").reply(200, { code: 0, data: agentRollout() })
 mock.onGet("/admin/agent-rollouts/rollout%2Fone/targets").reply(200, { code: 0, data: { items: [agentTarget()], next_cursor: "" } })
 mock.onGet("/nodes").reply(200, { code: 0, data: [] })
 mock.onGet("/nodes/manage").reply(200, { code: 0, data: [{ ...workspaceNode("private-id"), name: "Private database", is_public: false, tags: ["production"] }] })
 mock.onGet("/groups").reply(200, { code: 0, data: [{ id: "group-private", name: "Private group" }] })
})
async function tab(name: string) { await waitFor(() => expect(screen.getByRole("tab", { name })).toBeEnabled()); await userEvent.click(screen.getByRole("tab", { name })) }
async function choose(label: string, name: string) { await userEvent.click(screen.getByRole("combobox", { name: label })); await userEvent.click(await screen.findByRole("option", { name })) }
async function openCreate() { await tab("Upgrade batches"); await waitFor(() => expect(screen.getByRole("button", { name: "Create upgrade batch" })).toBeEnabled()); await userEvent.click(screen.getByRole("button", { name: "Create upgrade batch" })) }
describe("Agent release workspace", () => {
 it("restricts reads to owners and reports disabled modules without simulated releases", async () => {
  auth.principal.user.role = "admin"; const view = renderPage()
  expect(screen.getByRole("status")).toHaveTextContent("Only an owner"); expect(mock.history.get).toHaveLength(0)
  auth.principal.user.role = "owner"; mock.onGet("/admin/agent-rollout-status").reply(200, { code: 0, data: { enabled: false, configured: false, ready: false, reason: "disabled" } }); view.rerender(<MemoryRouter><AgentReleasesPage /></MemoryRouter>)
  expect(await screen.findByRole("status")).toHaveTextContent("This does not affect node setup")
  expect(screen.queryByText("No version packages imported")).not.toBeInTheDocument()
  expect(screen.queryByRole("tab", { name: "Upgrade batches" })).not.toBeInTheDocument()
  expect(document.querySelectorAll('a[href="/admin/agent-setup"]')).toHaveLength(2)
  expect(agentRolloutError("unknown", t)).toBe("Request failed"); expect(agentRolloutError(new APIError("denied", 403), t)).toBe("denied")
 })
 it("reports real disabled and unconfigured status before loading release data", async () => {
  mock.onGet("/admin/agent-rollout-status").reply(200, { code: 0, data: { enabled: false, configured: false, ready: false, reason: "disabled" } })
  renderPage(); expect(await screen.findByRole("status")).toHaveTextContent("Rollout module: disabled")
  expect(mock.history.get.map(request => request.url)).toEqual(["/admin/agent-rollout-status"])
  mock.onGet("/admin/agent-rollout-status").reply(200, { code: 0, data: { enabled: true, configured: false, ready: false, reason: "unconfigured" } })
  await userEvent.click(screen.getByRole("button", { name: "Retry status" })); await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("unconfigured"))
  mock.onGet("/admin/agent-rollout-status").reply(503, { message: "status unavailable" })
  await userEvent.click(screen.getByRole("button", { name: "Retry status" })); expect(await screen.findByRole("alert")).toHaveTextContent("status unavailable")
 })
 it("shows verified metadata, per-artifact readiness and signed platform support, then confirms revocation", async () => {
  mock.onPost().reply(200, { code: 0, data: agentRollout() })
  renderPage(); await userEvent.click(await screen.findByRole("button", { name: "View 2.0.0" }))
  expect(await screen.findByText("missing")).toBeInTheDocument(); expect(screen.getByText(/Expires 2099/)).toHaveTextContent("minimum server schema 15")
  expect(screen.queryByText("release/one")).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Revoke release" })); expect(mock.history.post).toHaveLength(0)
  await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(mock.history.post[0].url).toBe("/admin/agent-releases/release%2Fone/revoke"))
  await tab("Platform support"); expect(await screen.findByText("preview")).toBeInTheDocument(); expect(screen.queryByText("supported")).not.toBeInTheDocument()
 })
 it("preserves release pagination cursors and shows empty pages with back navigation", async () => {
  mock.onGet("/admin/agent-releases").reply(config => [200, { code: 0, data: config.params.cursor ? { items: [], next_cursor: "" } : { items: [agentRelease()], next_cursor: "opaque+/=" } }])
  renderPage(); await screen.findByRole("button", { name: "View 2.0.0" }); await userEvent.click(screen.getByRole("button", { name: "Next page" }))
  expect(await screen.findByText("No version packages imported")).toBeInTheDocument(); expect(mock.history.get.some(request => request.params?.cursor === "opaque+/=")).toBe(true)
  await userEvent.click(screen.getByRole("button", { name: "Previous page" })); expect(await screen.findByRole("button", { name: "View 2.0.0" })).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Refresh workspace" })); expect(mock.history.get.length).toBeGreaterThan(4)
 })
 it("freezes named private nodes, groups and tags only after rollback acknowledgement and confirmation", async () => {
  mock.onPost().reply(200, { code: 0, data: agentRollout() })
  renderPage(); await openCreate()
  fireEvent.change(screen.getByLabelText("Rollout name"), { target: { value: "Emergency rollback" } }); await choose("Release", "2.0.0 · stable"); await choose("Action", "Rollback to older release")
  await userEvent.click(await screen.findByRole("checkbox", { name: "Private database" })); await userEvent.click(screen.getByRole("checkbox", { name: "Private group" })); await userEvent.click(screen.getByRole("checkbox", { name: "production" }))
  fireEvent.change(screen.getByLabelText("Observation window (seconds)"), { target: { value: "600" } }); await userEvent.click(screen.getByRole("checkbox", { name: "Allow preview platforms" }))
  fireEvent.change(screen.getByLabelText("Maintenance starts (local time)"), { target: { value: "2098-01-01T00:00" } }); fireEvent.change(screen.getByLabelText("Maintenance ends (local time)"), { target: { value: "2098-01-02T00:00" } })
  await userEvent.click(screen.getByRole("button", { name: "Review frozen targets" })); expect(await screen.findByRole("alert")).toHaveTextContent("Explicitly acknowledge rollback")
  await userEvent.click(screen.getByRole("checkbox", { name: /I explicitly authorize rollback/ })); await userEvent.click(screen.getByRole("button", { name: "Review frozen targets" })); expect(mock.history.post).toHaveLength(0)
  await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(mock.history.post).toHaveLength(1))
  expect(JSON.parse(mock.history.post[0].data)).toMatchObject({ name: "Emergency rollback", release_id: "release/one", node_ids: ["private-id"], group_ids: ["group-private"], tags: ["production"], action: "rollback", acknowledge_rollback: true })
  expect(mock.history.get.some(request => request.url === "/nodes/manage")).toBe(true); expect(mock.history.get.some(request => request.url === "/nodes")).toBe(false)
 }, 20_000)
 it("shows snapshot held reasons and promotes only by explicit owner confirmation", async () => {
  mock.onPost().reply(200, { code: 0, data: agentRollout() })
  renderPage(); await tab("Upgrade batches"); await userEvent.click(await screen.findByRole("button", { name: "View Canary fleet" }))
  expect(await screen.findByText("Private database")).toBeInTheDocument(); expect(screen.getByText("external_manager")).toBeInTheDocument(); expect(screen.getByText(/This page:/)).toHaveTextContent("1 held")
  expect(screen.getByRole("button", { name: "Resume rollout" })).toBeDisabled(); await userEvent.click(screen.getByRole("button", { name: "Promote next wave" })); expect(mock.history.post).toHaveLength(0)
  await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(mock.history.post[0].url).toBe("/admin/agent-rollouts/rollout%2Fone/promote"))
 })
 it("requires uncertainty acknowledgement before resume and exposes conflict errors without success", async () => {
  mock.onGet("/admin/agent-rollouts/rollout%2Fone").reply(200, { code: 0, data: { ...agentRollout(), current_wave: 0, reason: "assignment_expired" } })
  mock.onPost("/admin/agent-rollouts/rollout%2Fone/resume").reply(409, { message: "health gate conflict" })
  renderPage(); await tab("Upgrade batches"); await userEvent.click(await screen.findByRole("button", { name: "View Canary fleet" })); await userEvent.click(await screen.findByRole("button", { name: "Resume rollout" }))
  expect(screen.getByRole("button", { name: "Confirm" })).toBeDisabled(); await userEvent.click(screen.getByRole("checkbox")); await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("health gate conflict"); expect(JSON.parse(mock.history.post[0].data)).toEqual({ acknowledge_uncertainty: true })
 })
 it("keeps terminal states immutable and validates health and maintenance bounds", () => {
  const target = { ...agentTarget(), healthy_since: "2026-10-08T00:00:00Z" }
  expect(agentObservationRemaining({ target, serverTime: "2026-10-08T00:01:00Z", seconds: 300, t })).toBe("240s remaining at server snapshot")
  expect(agentObservationRemaining({ target, serverTime: "2026-10-08T00:06:00Z", seconds: 300, t })).toContain("awaiting server decision")
  expect(agentObservationRemaining({ target, serverTime: "invalid", seconds: 300, t })).toBe("Observation time unavailable")
  expect(agentObservationRemaining({ target: agentTarget(), serverTime: undefined, seconds: 300, t })).toContain("Waiting")
  const value = agentRollout(); expect(canCommandAgentRollout({ ...value, state: "completed" }, "abort")).toBe(false); expect(canCommandAgentRollout(value, "abort")).toBe(true)
  expect(canCommandAgentRollout({ ...value, state: "active" }, "pause")).toBe(true); expect(canCommandAgentRollout({ ...value, state: "paused", reason: "health" }, "promote")).toBe(false)
  expect(validateAgentRolloutPolicy(value.policy, t)).toBe(""); expect(validateAgentRolloutPolicy({ ...value.policy, observation_seconds: 29 }, t)).toContain("30–3600")
  expect(validateAgentRolloutPolicy({ ...value.policy, window_end: "2020-01-01" }, t)).toContain("future maintenance"); expect(validateAgentRolloutPolicy({ ...value.policy, wave_size: 1.5 }, t)).toContain("Nodes per wave")
 })
 it("removes a finished rollout and a revoked release only after confirmation", async () => {
  const finished = { ...agentRollout(), state: "aborted", reason: "operator_abort" }
  mock.onGet("/admin/agent-rollouts/rollout%2Fone").reply(200, { code: 0, data: finished })
  mock.onGet("/admin/agent-releases/release%2Fone").reply(200, { code: 0, data: { ...agentRelease(), revoked: true } })
  mock.onDelete().reply(200, { code: 0, data: null })
  renderPage()
  expect(canDeleteAgentRollout(agentRollout())).toBe(false); expect(canDeleteAgentRollout(finished)).toBe(true)
  await userEvent.click(await screen.findByRole("button", { name: "View 2.0.0" }))
  await userEvent.click(await screen.findByRole("button", { name: "Delete release" }))
  expect(mock.history.delete).toHaveLength(0)
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
  await waitFor(() => expect(mock.history.delete[0].url).toBe("/admin/agent-releases/release%2Fone"))
  await waitFor(() => expect(screen.queryByRole("button", { name: "Delete release" })).not.toBeInTheDocument())
  await tab("Upgrade batches"); await userEvent.click(await screen.findByRole("button", { name: "View Canary fleet" }))
  await userEvent.click(await screen.findByRole("button", { name: "Delete rollout" }))
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
  await waitFor(() => expect(mock.history.delete[1].url).toBe("/admin/agent-rollouts/rollout%2Fone"))
 })
})
