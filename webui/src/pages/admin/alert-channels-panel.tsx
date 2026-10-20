import { ReauthDialog } from "@/components/reauth-dialog"
import { useNotificationChannels } from "@/hooks/use-notification-channels"
import { HistoryIcon, PencilIcon, PlusIcon, RefreshCwIcon, SendIcon, Trash2Icon } from "lucide-react"
import { setNotificationChannelEnabled } from "@/lib/notification-api"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { NotificationConfirmDialog } from "@/pages/admin/notification-confirm-dialog"
import { NotificationChannelHealth } from "@/pages/admin/notification-status"
import AlertChannelDialog from "@/pages/admin/alert-channel-dialog"
import { emptyAlertChannelForm } from "@/pages/admin/alert-channel-config"

export default function AlertChannelsPanel({ onHistory }: { onHistory?: (id: string) => void }) {
  const { t } = useLocale()
  const { list, owner, editing, open, setOpen, form, setForm, error, notice, preview, setPreview, disabled, edit, operation, save, remove, test, reauth } = useNotificationChannels()
  return <div className="grid gap-4">
    <div className="flex flex-wrap items-center justify-between gap-3"><p className="max-w-2xl text-sm text-muted-foreground">{t("notification.channels_description")}</p>
      <div className="flex gap-2"><Button variant="outline" disabled={list.loading || disabled} onClick={() => void list.refresh()} aria-label={t("notification.refresh")}><RefreshCwIcon /></Button>
        {owner.owner && <Button disabled={disabled} onClick={() => edit()}><PlusIcon />{t("notification.create_channel")}</Button>}</div>
    </div>
    {(list.error || (!open && (error || owner.error))) && <Alert variant="destructive" role="alert"><AlertDescription>{t(list.error || error || owner.error)}</AlertDescription></Alert>}
    {notice && <Alert role="status"><AlertDescription>{notice}</AlertDescription></Alert>}
    <div className="overflow-hidden rounded-lg border"><Table><TableHeader><TableRow>
      <TableHead>{t("node.name")}</TableHead><TableHead>{t("notification.provider")}</TableHead><TableHead>{t("notification.health")}</TableHead>
      <TableHead>{t("alert.enabled")}</TableHead><TableHead className="text-right">{t("app.actions")}</TableHead>
    </TableRow></TableHeader><TableBody>
      {list.loading && !list.data?.length ? <TableRow>{Array.from({ length: 5 }, (_, index) => <TableCell key={index}><Skeleton className="h-6 w-24" /></TableCell>)}</TableRow>
        : list.data?.length ? list.data.map(channel => <TableRow key={channel.id}>
          <TableCell><span className="font-medium">{channel.name}</span><p className="mt-1 text-xs text-muted-foreground">{t("notification.subscription_count", { count: channel.event_kinds.length })}</p></TableCell>
          <TableCell>{t(`notification.provider.${channel.channel_type}`)}</TableCell><TableCell><NotificationChannelHealth channel={channel} /></TableCell>
          <TableCell><Switch aria-label={t("notification.toggle", { name: channel.name })} checked={channel.enabled} disabled={disabled}
            onCheckedChange={enabled => void operation(async () => { await setNotificationChannelEnabled(channel.id, enabled) })} /></TableCell>
          <TableCell><div className="flex justify-end gap-1">
            <Button variant="outline" size="sm" disabled={disabled} onClick={() => void test(channel)}><SendIcon />{t("alert.test_send")}</Button>
            {onHistory && <Button variant="ghost" size="icon-sm" aria-label={t("notification.history_for", { name: channel.name })} onClick={() => onHistory(channel.id)}><HistoryIcon /></Button>}
            {owner.owner && <><Button variant="ghost" size="icon-sm" disabled={disabled} aria-label={t("notification.edit_named", { name: channel.name })} onClick={() => edit(channel)}><PencilIcon /></Button>
              <Button variant="ghost" size="icon-sm" disabled={disabled} aria-label={t("notification.delete_named", { name: channel.name })} onClick={() => remove(channel)}><Trash2Icon className="text-destructive" /></Button></>}
          </div></TableCell>
        </TableRow>) : <TableRow><TableCell colSpan={5} className="py-8 text-center text-muted-foreground">{t("notification.no_channels")}</TableCell></TableRow>}
    </TableBody></Table></div>
    {reauth.pending && <ReauthDialog codeRequired={reauth.codeRequired} pending={reauth.busy} error={reauth.error}
      onClose={reauth.close} onConfirm={(password, code) => void reauth.confirm(password, code)} />}
    <AlertChannelDialog open={open} editing={editing !== null} form={form} setForm={setForm} loading={disabled}
      onOpenChange={value => { setOpen(value); if (!value) { setForm(emptyAlertChannelForm()); setPreview(null) } }}
      onSave={() => save()} onPreview={() => save(true)} error={error || owner.error} preview={preview} />
    <NotificationConfirmDialog flow={owner} />
  </div>
}
