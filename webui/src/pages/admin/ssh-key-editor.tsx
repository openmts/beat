import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { useLocale } from "@/context/locale"
export type SSHKeyDraft = { mode: "import" | "generate"; name: string; publicKey: string; privateKey: string; keyType: string }
const types = [{ label: "Ed25519", value: "ed25519" }, { label: "RSA 2048", value: "rsa" }]
export function SSHKeyEditor({ close, submit }: { close: () => void; submit: (draft: SSHKeyDraft) => void }) {
 const { t } = useLocale(), [draft, setDraft] = useState<SSHKeyDraft>({ mode: "import", name: "", publicKey: "", privateKey: "", keyType: "ed25519" })
 const change = (field: keyof SSHKeyDraft, value: string) => setDraft(current => ({ ...current, [field]: value }))
 return <Dialog open onOpenChange={open => { if (!open) close() }}><DialogContent className="max-w-lg"><DialogHeader><DialogTitle>{t("ssh.keys")}</DialogTitle><DialogDescription>{t("ssh.keys_description")}</DialogDescription></DialogHeader>
  <Tabs value={draft.mode} onValueChange={mode => { change("mode", mode); change("privateKey", "") }}><TabsList className="w-full"><TabsTrigger value="import">{t("ssh.import")}</TabsTrigger><TabsTrigger value="generate">{t("ssh.generate")}</TabsTrigger></TabsList>
   <div className="my-4 space-y-2"><Label htmlFor="ssh-key-name">{t("node.name")}</Label><Input id="ssh-key-name" value={draft.name} onChange={e => change("name", e.target.value)} /></div>
   <TabsContent value="import" className="space-y-3"><Label htmlFor="ssh-import-public">{t("ssh.public_key")}</Label><Textarea id="ssh-import-public" value={draft.publicKey} onChange={e => change("publicKey", e.target.value)} />
    <Label htmlFor="ssh-import-private">{t("ssh.private_key")}</Label><Textarea id="ssh-import-private" autoComplete="off" spellCheck={false} value={draft.privateKey} onChange={e => change("privateKey", e.target.value)} /></TabsContent>
   <TabsContent value="generate"><Label>{t("ssh.key_type")}</Label><Select items={types} value={draft.keyType} onValueChange={value => { if (value) change("keyType", value) }}><SelectTrigger aria-label={t("ssh.key_type")}><SelectValue /></SelectTrigger><SelectContent>{types.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select></TabsContent>
  </Tabs><DialogFooter><Button variant="outline" onClick={close}>{t("app.cancel")}</Button><Button disabled={!draft.name.trim() || (draft.mode === "import" && !draft.publicKey.trim())} onClick={() => submit({ ...draft, name: draft.name.trim(), publicKey: draft.publicKey.trim(), privateKey: draft.privateKey.trim() })}>{t("app.confirm")}</Button></DialogFooter>
 </DialogContent></Dialog>
}
