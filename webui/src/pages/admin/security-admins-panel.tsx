import { useCallback, useEffect, useState, type FormEvent } from "react"
import { PlusIcon, Trash2Icon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog"
import { Checkbox } from "@/components/ui/checkbox"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useApiList } from "@/hooks/use-api-list"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { createAdminUser, deleteAdminUser, listAdminUsers, updateAdminUser } from "@/lib/security-api"
import { OperationConfirm } from "./operation-confirm"
import type { AdminRole, AdminUser } from "@/types/security"

type AccountDraft = { username: string; display_name: string; password: string; role: AdminRole }
const blankAccount = (): AccountDraft => ({ username: "", display_name: "", password: "", role: "admin" })

export function SecurityAdminsPanel() {
 const { principal } = useAuth(), { t } = useLocale()
 const owner = principal?.user.role === "owner", identity = owner ? `${principal.user.id}/${principal.session.id}` : ""
 const users = useApiList(useCallback(signal => identity ? listAdminUsers(signal) : Promise.resolve([]), [identity]))
 const flow = useConfirmedOperation(), [draft, setDraft] = useState(blankAccount), [editing, setEditing] = useState<AdminUser | null>(null)
 useEffect(() => { setDraft(blankAccount()); setEditing(null) }, [identity])
 useEffect(() => { if (!flow.action) setDraft(value => ({ ...value, password: "" })) }, [flow.action])
 if (!owner) return null
 const create = (event: FormEvent) => {
  event.preventDefault(); const input = { ...draft }; setDraft(value => ({ ...value, password: "" }))
  flow.request({ title: t("security.create_admin"), description: t("security.create_admin_description"), execute: async () => {
   if (!input.password) throw new Error(t("security.password_missing"))
   try { await createAdminUser({ ...input }); setDraft(blankAccount()); await users.refresh() } finally { input.password = ""; setDraft(value => ({ ...value, password: "" })) }
  } })
 }
 const remove = (user: AdminUser) => flow.request({ title: `${t("app.delete")} · ${user.display_name}`, description: t("security.delete_admin_description"), execute: async () => { await deleteAdminUser(user.id); await users.refresh() } })
 const save = (user: AdminUser) => flow.request({ title: t("security.save_admin_title"), description: t("security.save_admin_description"), execute: async () => { await updateAdminUser(user.id, { display_name: user.display_name, role: user.role, enabled: user.enabled }); setEditing(null); await users.refresh() } })
 return <div className="space-y-4">{users.error && <Alert variant="destructive"><AlertDescription>{users.error}</AlertDescription></Alert>}<div className="grid gap-4 xl:grid-cols-[minmax(18rem,24rem)_1fr]">
  <CreateAccountForm draft={draft} change={setDraft} submit={create} busy={flow.busy} />
  <div className="grid content-start gap-3 md:grid-cols-2">{users.data?.map(user => <Card key={user.id}><CardHeader><div className="flex items-center justify-between gap-2"><CardTitle className="truncate text-sm">{user.display_name}</CardTitle><Badge variant={user.enabled ? "default" : "secondary"}>{user.enabled ? t("security.enabled") : t("security.disabled")}</Badge></div><p className="text-sm text-muted-foreground">@{user.username} · {t(`security.${user.role}`)}</p></CardHeader><CardContent className="flex justify-end gap-2"><Button variant="outline" onClick={() => setEditing(user)}>{t("app.edit")}</Button><Button variant="ghost" size="icon-sm" aria-label={t("app.delete")} disabled={user.id === principal?.user.id} onClick={() => remove(user)}><Trash2Icon /></Button></CardContent></Card>)}</div>
 </div>{editing && <EditAccount key={editing.id} initial={editing} close={() => setEditing(null)} save={save} />}<OperationConfirm flow={flow} /></div>
}

function CreateAccountForm({ draft, change, submit, busy }: { draft: AccountDraft; change: (value: AccountDraft) => void; submit: (event: FormEvent) => void; busy: boolean }) {
 const { t } = useLocale()
 return <Card><CardHeader><CardTitle>{t("security.create_admin")}</CardTitle></CardHeader><CardContent><form onSubmit={submit} className="space-y-4">
  <label className="block space-y-1 text-sm">{t("security.username")}<Input value={draft.username} onChange={event => change({ ...draft, username: event.target.value })} required /></label>
  <label className="block space-y-1 text-sm">{t("security.display_name")}<Input value={draft.display_name} onChange={event => change({ ...draft, display_name: event.target.value })} required /></label>
  <label className="block space-y-1 text-sm">{t("security.password")}<Input type="password" autoComplete="new-password" minLength={12} value={draft.password} onChange={event => change({ ...draft, password: event.target.value })} required /></label>
  <AccountRole value={draft.role} change={role => change({ ...draft, role })} />
  <Button type="submit" disabled={busy}><PlusIcon />{t("security.create_admin")}</Button>
 </form></CardContent></Card>
}

function AccountRole({ value, change }: { value: AdminRole; change: (value: AdminRole) => void }) {
 const { t } = useLocale(), options = [{ value: "admin", label: t("security.admin") }, { value: "owner", label: t("security.owner") }]
 return <label className="block space-y-1 text-sm">{t("security.role")}<Select items={options} value={value} onValueChange={role => { if (role) change(role as AdminRole) }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{options.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select></label>
}

function EditAccount({ initial, close, save }: { initial: AdminUser; close: () => void; save: (value: AdminUser) => void }) {
 const { t } = useLocale(), [value, setValue] = useState(initial)
 return <Dialog open onOpenChange={open => { if (!open) close() }}><DialogContent><DialogHeader><DialogTitle>{t("security.edit_admin_title")} · @{value.username}</DialogTitle></DialogHeader><label className="space-y-1 text-sm">{t("security.display_name")}<Input value={value.display_name} onChange={event => setValue({ ...value, display_name: event.target.value })} /></label><AccountRole value={value.role} change={role => setValue({ ...value, role })} /><label className="flex items-center gap-2 text-sm"><Checkbox checked={value.enabled} onCheckedChange={enabled => setValue({ ...value, enabled: Boolean(enabled) })} />{t("security.enabled")}</label><DialogFooter><Button variant="outline" onClick={close}>{t("app.cancel")}</Button><Button disabled={!value.display_name.trim()} onClick={() => save(value)}>{t("app.save")}</Button></DialogFooter></DialogContent></Dialog>
}
