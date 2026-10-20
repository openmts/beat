import { useState } from "react"
import { CopyIcon, CheckIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import type { GeneratedSSHKey } from "@/types"
export function SSHKeyDetail({ value, close }: { value: GeneratedSSHKey; close: () => void }) {
 const { t } = useLocale()
 return <Dialog open onOpenChange={open => { if (!open) close() }}><DialogContent className="max-w-2xl"><DialogHeader><DialogTitle>{value.name}</DialogTitle><DialogDescription>{t("ssh.generated_description")}</DialogDescription></DialogHeader>
  <KeyField label={t("ssh.fingerprint")} value={value.fingerprint} /><KeyField label={t("ssh.public_key")} value={value.public_key} /><KeyField label={t("ssh.private_key")} value={value.private_key || ""} />
  <DialogFooter><Button variant="outline" onClick={close}>{t("app.cancel")}</Button></DialogFooter></DialogContent></Dialog>
}
function KeyField({ label, value }: { label: string; value: string }) {
 const { t } = useLocale(), [copied, setCopied] = useState(false), [error, setError] = useState("")
 const copy = async () => { setError(""); try { await navigator.clipboard.writeText(value); setCopied(true) } catch (error) { setError(error instanceof Error ? error.message : t("app.error")) } }
 return <div className="min-w-0"><div className="mb-1.5 flex items-center justify-between gap-2"><Label>{label}</Label><Button variant="ghost" size="sm" disabled={!value} onClick={() => void copy()} aria-label={`${t("ssh.copy_key")} · ${label}`}>{copied ? <CheckIcon /> : <CopyIcon />}{copied ? t("ssh.copied") : t("ssh.copy_key")}</Button></div><pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted p-3 text-xs">{value || "--"}</pre>{error && <p role="alert" className="text-destructive">{error}</p>}</div>
}
