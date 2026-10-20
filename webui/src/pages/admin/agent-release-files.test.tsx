import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest"
import AxiosMockAdapter from "axios-mock-adapter"
import { api } from "@/lib/api-client"
import { agentRelease } from "@/test/agent-rollout-fixtures"
import { AgentArtifactUpload, AgentReleaseImport } from "./agent-release-files"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  const value = testLocaleValue("en")
  return { useLocale: () => value, useOptionalLocale: () => value }
})
const mock = new AxiosMockAdapter(api)
beforeEach(() => mock.reset())
afterAll(() => mock.restore())
function binaryFile(text: string, name: string) {
 const file = new File([text], name), bytes = new TextEncoder().encode(text)
 Object.defineProperty(file, "arrayBuffer", { value: async () => bytes.slice().buffer })
 Object.defineProperty(file, "slice", { value: (start: number, end: number) => ({ arrayBuffer: async () => bytes.slice(start, end).buffer }) })
 return file
}
async function chooseUpload(text = "abc") { await userEvent.upload(screen.getByLabelText("Artifact file: beat-agent"), binaryFile(text, "beat-agent")); await userEvent.click(screen.getByRole("button", { name: "Upload artifact" })); await userEvent.click(screen.getByRole("button", { name: "Confirm" })) }
describe("Agent signed files", () => {
 it("imports unchanged signing files only after owner confirmation and reports local input errors", async () => {
  const saved = vi.fn().mockResolvedValue(undefined); mock.onPost("/admin/agent-releases/import").reply(200, { code: 0, data: agentRelease() })
  render(<AgentReleaseImport saved={saved} />); expect(screen.getByRole("button", { name: "Verify and import" })).toBeDisabled()
  await userEvent.upload(screen.getByLabelText("Manifest JSON"), binaryFile('{"v":1}\n', "manifest.json")); await userEvent.upload(screen.getByLabelText("Ed25519 signature"), binaryFile("bad", "signature")); await userEvent.upload(screen.getByLabelText("Sigstore bundle"), binaryFile("{}", "bundle"))
  await userEvent.click(screen.getByRole("button", { name: "Verify and import" })); expect(await screen.findByRole("alert")).toHaveTextContent("64 raw bytes")
  await userEvent.upload(screen.getByLabelText("Ed25519 signature"), binaryFile("s".repeat(64), "signature")); await userEvent.click(screen.getByRole("button", { name: "Verify and import" })); await screen.findByRole("dialog"); expect(mock.history.post).toHaveLength(0)
  await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(saved).toHaveBeenCalledOnce()); expect(JSON.parse(mock.history.post[0].data).payload).toBe(btoa('{"v":1}\n'))
 })
 it("verifies and uploads an artifact with progress while refreshing real server state", async () => {
  const saved = vi.fn().mockResolvedValue(undefined); mock.onPut().reply(config => { config.onUploadProgress?.({ loaded: 3, bytes: 3, lengthComputable: true }); return [200, { code: 0, data: null }] })
  render(<AgentArtifactUpload releaseID="release" artifact={agentRelease().manifest.artifacts[0]} state="missing" disabled={false} saved={saved} />)
  await chooseUpload(); await waitFor(() => expect(saved).toHaveBeenCalledOnce()); expect(screen.getByRole("status")).toHaveTextContent("Upload accepted"); expect(screen.getByRole("progressbar")).toHaveAttribute("value", "100")
  expect(mock.history.put).toHaveLength(1)
 })
 it("rejects wrong artifacts without uploading and honors disabled controls", async () => {
  const props = { releaseID: "release", artifact: agentRelease().manifest.artifacts[0], state: "missing", disabled: false, saved: vi.fn() }
  const view = render(<AgentArtifactUpload {...props} />); await chooseUpload("bad"); expect(await screen.findByRole("alert")).toHaveTextContent("SHA-256"); expect(mock.history.put).toHaveLength(0)
  fireEvent.change(screen.getByLabelText("Artifact file: beat-agent"), { target: { files: [] } }); expect(screen.getByRole("button", { name: "Upload artifact" })).toBeDisabled()
  view.rerender(<AgentArtifactUpload {...props} disabled />); expect(screen.getByLabelText("Artifact file: beat-agent")).toBeDisabled()
 })
 it("cancels an in-flight upload without claiming it was rolled back", async () => {
  let finish!: () => void; mock.onPut().reply(() => new Promise(resolve => { finish = () => resolve([200, { code: 0, data: null }]) }))
  render(<AgentArtifactUpload releaseID="release" artifact={agentRelease().manifest.artifacts[0]} state="missing" disabled={false} saved={vi.fn()} />)
  await chooseUpload(); const cancel = await screen.findByRole("button", { name: "Cancel upload" }); await waitFor(() => expect(mock.history.put).toHaveLength(1)); await userEvent.click(cancel); finish()
  expect(await screen.findByRole("alert")).toHaveTextContent("server may already have accepted")
 })
 it("requires fresh proof after a 428 and never automatically retries the upload", async () => {
  mock.onPut().reply(428, { message: "recent proof required" })
  render(<AgentArtifactUpload releaseID="release" artifact={agentRelease().manifest.artifacts[0]} state="missing" disabled={false} saved={vi.fn()} />)
  await chooseUpload(); expect(await screen.findByRole("alert")).toHaveTextContent("recent proof required"); expect(mock.history.put).toHaveLength(1)
  await userEvent.click(screen.getByRole("button", { name: "Upload artifact" })); expect(await screen.findByLabelText("Current password")).toBeInTheDocument(); expect(screen.getByRole("button", { name: "Confirm" })).toBeDisabled(); expect(mock.history.put).toHaveLength(1)
 })
})
