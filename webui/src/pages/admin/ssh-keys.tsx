import { useState } from "react"
import { PlusIcon, Trash2Icon, EyeIcon, CopyIcon } from "lucide-react"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useSSHKeys } from "@/hooks/use-api"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import * as api from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { PageHeader } from "@/components/page-header"
import { OperationConfirm } from "./operation-confirm"
import { SSHKeyEditor, type SSHKeyDraft } from "./ssh-key-editor"
import { SSHKeyDetail } from "./ssh-key-detail"
import type { GeneratedSSHKey, SSHKey } from "@/types"

export default function SSHKeys() {
 const { principal } = useAuth()
 return <SSHKeysSession key={`${principal?.user.id}/${principal?.session.id}`} />
}
function SSHKeysSession() {
 const { data: keys, loading, error, refresh } = useSSHKeys(), { t } = useLocale()
 const flow = useConfirmedOperation(), [editing, setEditing] = useState(false), [detail, setDetail] = useState<GeneratedSSHKey | null>(null), [copyError, setCopyError] = useState("")
 const create = (draft: SSHKeyDraft) => {
  setEditing(false)
  flow.request({ title: `${t(draft.mode === "import" ? "ssh.import" : "ssh.generate")} · ${draft.name}`, description: t("ssh.save_description"), execute: async () => {
   if (draft.mode === "generate") setDetail(await api.generateSSHKey(draft.name, draft.keyType))
   else await api.createSSHKey({ name: draft.name, public_key: draft.publicKey, private_key: draft.privateKey || undefined, key_type: "imported" })
   draft.privateKey = ""; await refresh()
  } })
 }
 const remove = (key: SSHKey) => flow.request({ title: `${t("app.delete")} · ${key.name}`, description: t("ssh.delete_description"), execute: async () => { await api.deleteSSHKey(key.id); await refresh() } })
 const reveal = (key: SSHKey) => flow.request({ title: `${t("ssh.private_key")} · ${key.name}`, description: t("ssh.reveal_description"), execute: async () => { setDetail(await api.getSSHKey(key.id)) } })
 const copy = async (value: string) => { setCopyError(""); try { await navigator.clipboard.writeText(value) } catch (error) { setCopyError(error instanceof Error ? error.message : t("app.error")) } }
 return <div className="flex flex-col gap-4"><PageHeader title={t("ssh.keys")} description={t("ssh.keys_description")}><Button onClick={() => setEditing(true)} disabled={flow.busy}><PlusIcon />{t("app.create")}</Button></PageHeader>
  {(error || copyError) && <Alert variant="destructive"><AlertDescription>{error || copyError}</AlertDescription></Alert>}
  <SSHKeyTable keys={keys || []} loading={loading} busy={flow.busy} remove={remove} reveal={reveal} copy={copy} />
  {editing && <SSHKeyEditor close={() => setEditing(false)} submit={create} />}{detail && <SSHKeyDetail value={detail} close={() => setDetail(null)} />}<OperationConfirm flow={flow} />
 </div>
}
function SSHKeyTable({ keys, loading, busy, remove, reveal, copy }: { keys: SSHKey[]; loading: boolean; busy: boolean; remove: (key: SSHKey) => void; reveal: (key: SSHKey) => void; copy: (value: string) => Promise<void> }) {
 const { t } = useLocale()
 return <Card><CardContent className="p-0"><Table><TableHeader><TableRow><TableHead>{t("node.name")}</TableHead><TableHead>{t("app.type")}</TableHead><TableHead>{t("ssh.fingerprint")}</TableHead><TableHead>{t("app.actions")}</TableHead></TableRow></TableHeader><TableBody>
  {loading ? <TableRow><TableCell colSpan={4}><Skeleton className="h-12 w-full" /></TableCell></TableRow> : keys.map(key => <TableRow key={key.id}><TableCell className="max-w-48 truncate font-medium">{key.name}</TableCell><TableCell>{key.key_type}</TableCell><TableCell className="max-w-48 truncate font-mono text-xs">{key.fingerprint || "--"}</TableCell><TableCell><div className="flex gap-1">
   <Button variant="ghost" size="icon-sm" onClick={() => void copy(key.public_key)} aria-label={t("ssh.copy_key")} title={t("ssh.copy_key")}><CopyIcon /></Button>
   <Button variant="ghost" size="icon-sm" disabled={busy} onClick={() => reveal(key)} aria-label={`${t("ssh.private_key")} · ${key.name}`} title={t("ssh.private_key")}><EyeIcon /></Button>
   <Button variant="ghost" size="icon-sm" disabled={busy} onClick={() => remove(key)} aria-label={`${t("app.delete")} · ${key.name}`} title={t("app.delete")}><Trash2Icon className="text-destructive" /></Button>
  </div></TableCell></TableRow>)}{!loading && !keys.length && <TableRow><TableCell colSpan={4} className="text-center text-muted-foreground">{t("app.no_data")}</TableCell></TableRow>}
 </TableBody></Table></CardContent></Card>
}
