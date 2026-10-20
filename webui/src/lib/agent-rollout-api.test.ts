import AxiosMockAdapter from "axios-mock-adapter"
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "./api-client"
import * as rollout from "./agent-rollout-api"
import { agentRelease, agentRollout, agentTarget } from "@/test/agent-rollout-fixtures"
const mock = new AxiosMockAdapter(api)
beforeEach(() => mock.reset())
afterAll(() => mock.restore())
function fileOf(bytes: string | Uint8Array) {
 const file = new File([typeof bytes === "string" ? bytes : Uint8Array.from(bytes).buffer], "artifact")
 const data = typeof bytes === "string" ? new TextEncoder().encode(bytes) : bytes
 Object.defineProperty(file, "arrayBuffer", { value: async () => data.slice().buffer })
 Object.defineProperty(file, "slice", { value: (start: number, end: number) => ({ arrayBuffer: async () => data.slice(start, end).buffer }) })
 return file
}
describe("Agent rollout HTTP adapter", () => {
 it("unwraps paginated lists and preserves opaque cursors and encoded resource IDs", async () => {
  const signal = new AbortController().signal, release = agentRelease(), value = agentRollout(), target = agentTarget()
  mock.onGet("/admin/agent-releases").reply(200, { code: 0, data: { items: [release], next_cursor: "next+opaque" } })
  mock.onGet("/admin/agent-rollouts").reply(200, { code: 0, data: { items: [value], next_cursor: "" } })
  mock.onGet("/admin/agent-releases/release%2Fone").reply(200, { code: 0, data: release })
  mock.onGet("/admin/agent-rollouts/rollout%2Fone").reply(200, { code: 0, data: value })
  mock.onGet("/admin/agent-rollouts/rollout%2Fone/targets").reply(200, { code: 0, data: { items: [target], next_cursor: "" } })
  const remote = { repository: "openmts/beat", items: [{ tag_name: "v3.0.0", version: "3.0.0", channel: "stable", name: "Agent 3", published_at: "2026-10-20T00:00:00Z", prerelease: false, ready: true, reason: "ready", assets: [] }], next_page: 3 }
  mock.onGet("/admin/agent-releases/remote").reply(200, { code: 0, data: remote })
  mock.onPost("/admin/agent-releases/remote/v3.0.0%2Bbuild/sync").reply(200, { code: 0, data: release })
  expect(await rollout.listAgentReleases("opaque+/=", signal)).toEqual({ items: [release], next_cursor: "next+opaque" })
  expect((await rollout.listAgentRollouts("", signal)).items).toEqual([value])
  expect(await rollout.getAgentRelease(release.id, signal)).toEqual(release)
  expect(await rollout.getAgentRollout(value.id, signal)).toEqual(value)
  expect((await rollout.listAgentTargets(value.id, "next", signal)).items).toEqual([target])
  expect(await rollout.listAgentRemoteReleases(2, signal)).toEqual(remote)
  expect(await rollout.syncAgentRemoteRelease("v3.0.0+build")).toEqual(release)
  expect(mock.history.get[0].params).toEqual({ limit: 50, cursor: "opaque+/=" })
  expect(mock.history.get[4].params).toEqual({ limit: 50, cursor: "next" })
  expect(mock.history.get[5].params).toEqual({ page: 2 })
  expect(mock.history.post[0].url).toBe("/admin/agent-releases/remote/v3.0.0%2Bbuild/sync")
 })
 it("sends signed imports, rollback acknowledgement and each command without response invention", async () => {
  mock.onPost().reply(200, { code: 0, data: agentRollout() })
  mock.onDelete().reply(200, { code: 0, data: null })
  const signed = { payload: "e30=", signature: "AA==", sigstore_bundle: "e30=" }
  await rollout.importAgentRelease(signed); await rollout.revokeAgentRelease("release/one")
  await rollout.deleteAgentRelease("release/one"); await rollout.deleteAgentRollout("rollout/one")
  await rollout.createAgentRollout({ name: "rollback", release_id: "release", node_ids: ["private"], group_ids: [], tags: [], policy: agentRollout().policy, action: "rollback", acknowledge_rollback: true })
  for (const command of ["promote", "pause", "resume", "abort"] as const) await rollout.commandAgentRollout("rollout/one", command)
  expect(JSON.parse(mock.history.post[0].data)).toEqual(signed)
  expect(JSON.parse(mock.history.post[2].data)).toMatchObject({ action: "rollback", acknowledge_rollback: true })
  expect(JSON.parse(mock.history.post[5].data)).toEqual({ acknowledge_uncertainty: true })
  expect(mock.history.post.map(request => request.url)).toContain("/admin/agent-rollouts/rollout%2Fone/abort")
  expect(mock.history.delete.map(request => request.url)).toEqual(["/admin/agent-releases/release%2Fone", "/admin/agent-rollouts/rollout%2Fone"])
 })
 it("hashes bounded slices and uploads the original File only when signed size and digest match", async () => {
  const file = fileOf("abc"), artifact = agentRelease().manifest.artifacts[0], progress = vi.fn(), signal = new AbortController().signal
  mock.onPut().reply(config => { config.onUploadProgress?.({ loaded: 3, bytes: 3, lengthComputable: true }); expect(config.data).toBe(file); return [200, { code: 0, data: null }] })
  await rollout.uploadAgentArtifact({ releaseID: "release/one", artifact, file }, { signal, progress })
  expect(progress).toHaveBeenCalledWith("hash", 3); expect(progress).toHaveBeenCalledWith("upload", 0); expect(progress).toHaveBeenCalledWith("upload", 3)
  expect(mock.history.put[0].url).toBe("/admin/agent-releases/release%2Fone/artifacts/linux%2Famd64")
  expect(mock.history.put[0].headers?.["Content-Type"]).toBe("application/octet-stream")
  await expect(rollout.uploadAgentArtifact({ releaseID: "r", artifact, file: fileOf("wrong") }, { signal, progress })).rejects.toThrow("agentRollout.error_artifact_size")
  await expect(rollout.uploadAgentArtifact({ releaseID: "r", artifact, file: fileOf("abd") }, { signal, progress })).rejects.toThrow("agentRollout.error_artifact_hash")
  const controller = new AbortController(); controller.abort()
  await expect(rollout.uploadAgentArtifact({ releaseID: "r", artifact, file }, { signal: controller.signal, progress })).rejects.toThrow()
  expect(mock.history.put).toHaveLength(1)
 })
 it("preserves canonical signed bytes and rejects invalid or oversized signing files", async () => {
  const manifest = fileOf('{"version":"1.0.0"}\n'), signature = fileOf(new Uint8Array(64)), bundle = fileOf("sigstore")
  expect(await rollout.readSignedAgentManifest({ manifest, signature, bundle })).toEqual({ payload: btoa('{"version":"1.0.0"}\n'), signature: btoa("\0".repeat(64)), sigstore_bundle: btoa("sigstore") })
  await expect(rollout.readSignedAgentManifest({ manifest, signature: fileOf("bad"), bundle })).rejects.toThrow("agentRollout.error_signature_bytes")
  await expect(rollout.readSignedAgentManifest({ manifest: fileOf(""), signature, bundle })).rejects.toThrow("agentRollout.error_signing_file")
  const tooLarge = fileOf("x"); Object.defineProperty(tooLarge, "size", { value: 2 * 1024 * 1024 + 1 })
  await expect(rollout.readSignedAgentManifest({ manifest: tooLarge, signature, bundle })).rejects.toThrow("agentRollout.error_signing_file")
  await expect(rollout.readSignedAgentManifest({ manifest: fileOf("x".repeat(800_000)), signature, bundle: fileOf("x".repeat(800_000)) })).rejects.toThrow("agentRollout.error_request_size")
 })
})
