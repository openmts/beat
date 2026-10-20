import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { formatInteger } from "@/lib/intl-format"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { APIError } from "@/lib/api-error"
import { agentRolloutError } from "@/hooks/use-agent-rollout-data"
import { AgentRolloutKeyedError, importAgentRelease, readSignedAgentManifest, uploadAgentArtifact } from "@/lib/agent-rollout-api"
import type { AgentArtifact } from "@/types/agent-rollouts"
import { OperationConfirm } from "./operation-confirm"
export function AgentReleaseImport({ saved }: { saved: () => Promise<unknown> }) {
 const { t } = useLocale(), flow = useConfirmedOperation(), [files, setFiles] = useState<Partial<Record<"manifest" | "signature" | "bundle", File>>>({}), [error, setError] = useState("")
 const prepare = async () => {
  setError("")
  try {
   const { manifest, signature, bundle } = files
   if (!manifest || !signature || !bundle) throw new AgentRolloutKeyedError("agentRollout.choose_signing_files")
   const signed = await readSignedAgentManifest({ manifest, signature, bundle })
   flow.request({ title: t("agentRollout.import_release"), description: t("agentRollout.import_description"), execute: async () => { await importAgentRelease(signed); await saved() } })
  } catch (error) { setError(agentRolloutError(error, t)) }
 }
 return <section className="space-y-4 rounded-lg border p-4"><h2 className="font-semibold">{t("agentRollout.import_release")}</h2><p className="text-sm text-muted-foreground">{t("agentRollout.import_upload_hint")}</p>
  {([['manifest', 'agentRollout.file_manifest'], ['signature', 'agentRollout.file_signature'], ['bundle', 'agentRollout.file_bundle']] as const).map(([key, label]) => <div key={key} className="space-y-2"><Label htmlFor={`release-${key}`}>{t(label)}</Label><Input id={`release-${key}`} type="file" disabled={flow.busy} onChange={event => setFiles(previous => ({ ...previous, [key]: event.target.files?.[0] }))} /></div>)}
  {error && <p role="alert" className="text-destructive">{error}</p>}<Button disabled={!files.manifest || !files.signature || !files.bundle || flow.busy} onClick={() => void prepare()}>{t("agentRollout.verify_and_import")}</Button><OperationConfirm flow={flow} />
 </section>
}
export function AgentArtifactUpload({ releaseID, artifact, state, disabled, saved }: { releaseID: string; artifact: AgentArtifact; state: string; disabled: boolean; saved: () => Promise<unknown> }) {
 const { locale, t } = useLocale()
 const [forceProof, setForceProof] = useState(false)
 const flow = useConfirmedOperation(), request = useRef<AbortController | null>(null)
 const [file, setFile] = useState<File | null>(null), [status, setStatus] = useState(""), [progress, setProgress] = useState(0), [busy, setBusy] = useState(false), [error, setError] = useState("")
 useEffect(() => () => request.current?.abort(), [])
 const upload = async () => {
  if (!file) return
  const controller = new AbortController(); request.current = controller; setBusy(true); setError(""); setProgress(0)
  try {
   await uploadAgentArtifact({ releaseID, artifact, file }, { signal: controller.signal, progress: (stage, bytes) => { setStatus(stage === "hash" ? t("agentRollout.verifying_hash") : t("agentRollout.uploading")); setProgress(Math.round(bytes / file.size * 100)) } })
   setStatus(t("agentRollout.upload_accepted")); await saved()
  } catch (error) { if (error instanceof APIError && error.status === 428) setForceProof(true); setError(controller.signal.aborted ? t("agentRollout.upload_canceled") : agentRolloutError(error, t)) }
  finally { setBusy(false); request.current = null }
 }
 return <div className="space-y-3 rounded-lg border p-4"><div className="flex flex-wrap items-center gap-2"><h3 className="break-all font-medium">{artifact.filename}</h3><Badge variant="outline">{state}</Badge><Badge variant="outline">{artifact.os}/{artifact.arch} · {artifact.support}</Badge></div><p className="text-sm">{t("agentRollout.artifact_size", { size: formatInteger(locale, artifact.size), kind: artifact.kind })}</p><p className="break-all font-mono text-xs text-muted-foreground">{t("agentRollout.artifact_sha256", { hash: artifact.sha256 })}</p>
  <Label htmlFor={`artifact-${artifact.id}`}>{t("agentRollout.artifact_file", { filename: artifact.filename })}</Label><Input id={`artifact-${artifact.id}`} type="file" disabled={disabled || busy} onChange={event => { setFile(event.target.files?.[0] ?? null); setError("") }} />
  <div className="flex gap-2"><Button variant="outline" disabled={disabled || busy || !file} onClick={() => flow.request({ title: t("agentRollout.upload_title", { filename: artifact.filename }), description: t("agentRollout.upload_description"), forceProof, execute: async () => { setForceProof(false); void upload() } })}>{t("agentRollout.upload_artifact")}</Button>{busy && <Button variant="outline" onClick={() => request.current?.abort()}>{t("agentRollout.cancel_upload")}</Button>}</div>
  {status && <div role="status" className="space-y-1 text-sm"><p>{status}</p><progress className="w-full" max={100} value={progress} aria-label={t("agentRollout.artifact_progress", { filename: artifact.filename })} /></div>}{error && <p role="alert" className="text-sm text-destructive">{error}</p>}<OperationConfirm flow={flow} />
 </div>
}
