import { api } from "@/lib/api-client"
import { hashOperationFile } from "@/lib/operation-transfers"
import type { AgentArtifact, AgentPage, AgentRelease, AgentReleaseDetail, AgentRollout, AgentRolloutCommand, AgentRolloutInput, AgentRolloutTarget, AgentRolloutStatus, SignedAgentManifest } from "@/types/agent-rollouts"
const releases = "/admin/agent-releases", rollouts = "/admin/agent-rollouts"
// AgentRolloutKeyedError 携带文案键：本地校验在非组件模块里执行，翻译留给展示层。
export class AgentRolloutKeyedError extends Error {
 readonly key: string
 constructor(key: string) { super(key); this.key = key }
}
const resource = (base: string, id: string) => `${base}/${encodeURIComponent(id)}`
export async function getAgentRolloutStatus(signal: AbortSignal) { return (await api.get<AgentRolloutStatus>("/admin/agent-rollout-status", { signal })).data }
export async function listAgentReleases(cursor: string, signal: AbortSignal) { return (await api.get<AgentPage<AgentRelease>>(releases, { params: { limit: 50, cursor }, signal })).data }
export async function listAgentRollouts(cursor: string, signal: AbortSignal) { return (await api.get<AgentPage<AgentRollout>>(rollouts, { params: { limit: 50, cursor }, signal })).data }
export async function getAgentRelease(id: string, signal: AbortSignal) { return (await api.get<AgentReleaseDetail>(resource(releases, id), { signal })).data }
export async function getAgentRollout(id: string, signal: AbortSignal) { return (await api.get<AgentRollout>(resource(rollouts, id), { signal })).data }
export async function listAgentTargets(id: string, cursor: string, signal: AbortSignal) { return (await api.get<AgentPage<AgentRolloutTarget>>(`${resource(rollouts, id)}/targets`, { params: { limit: 50, cursor }, signal })).data }
export async function importAgentRelease(input: SignedAgentManifest) { return (await api.post<AgentRelease>(`${releases}/import`, input)).data }
export async function revokeAgentRelease(id: string) { await api.post(`${resource(releases, id)}/revoke`) }
export async function deleteAgentRelease(id: string) { await api.delete(resource(releases, id)) }
export async function createAgentRollout(input: AgentRolloutInput) { return (await api.post<AgentRollout>(rollouts, input)).data }
export async function commandAgentRollout(id: string, command: AgentRolloutCommand) { await api.post(`${resource(rollouts, id)}/${command}`, command === "resume" ? { acknowledge_uncertainty: true } : undefined) }
export async function deleteAgentRollout(id: string) { await api.delete(resource(rollouts, id)) }
export const maximumAgentArtifactBytes = 512 * 1024 * 1024
export async function uploadAgentArtifact(input: { releaseID: string; artifact: AgentArtifact; file: File }, options: { signal: AbortSignal; progress: (stage: "hash" | "upload", bytes: number) => void }) {
 const { releaseID, artifact, file } = input
 if (file.size !== artifact.size || file.size < 1 || file.size > maximumAgentArtifactBytes) throw new AgentRolloutKeyedError("agentRollout.error_artifact_size")
 const digest = await hashOperationFile(file, { signal: options.signal, progress: bytes => options.progress("hash", bytes) })
 if (digest !== artifact.sha256) throw new AgentRolloutKeyedError("agentRollout.error_artifact_hash")
 options.signal.throwIfAborted()
 options.progress("upload", 0)
 await api.put(`${resource(releases, releaseID)}/artifacts/${encodeURIComponent(artifact.id)}`, file, { signal: options.signal, headers: { "Content-Type": "application/octet-stream" }, onUploadProgress: event => options.progress("upload", Math.min(event.loaded, file.size)) })
}
const maximumSignedJSONBytes = 2 * 1024 * 1024
async function base64File(file: File) {
 if (!file.size || file.size > maximumSignedJSONBytes) throw new AgentRolloutKeyedError("agentRollout.error_signing_file")
 const bytes = new Uint8Array(await file.arrayBuffer())
 let binary = ""
 for (const byte of bytes) binary += String.fromCharCode(byte)
 return btoa(binary)
}
export async function readSignedAgentManifest(files: { manifest: File; signature: File; bundle: File }): Promise<SignedAgentManifest> {
 if (files.signature.size !== 64) throw new AgentRolloutKeyedError("agentRollout.error_signature_bytes")
 const [payload, signature, sigstore_bundle] = await Promise.all([base64File(files.manifest), base64File(files.signature), base64File(files.bundle)])
 const signed = { payload, signature, sigstore_bundle }
 if (JSON.stringify(signed).length > maximumSignedJSONBytes) throw new AgentRolloutKeyedError("agentRollout.error_request_size")
 return signed
}
