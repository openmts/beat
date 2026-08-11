import { useState } from "react"
import { LoaderCircleIcon, PencilIcon, PlusIcon, SendIcon, Trash2Icon } from "lucide-react"
import { useAlertChannels } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type { AlertChannel } from "@/types"
import { messageFromError } from "@/pages/admin/alert-utils"
import AlertChannelDialog from "@/pages/admin/alert-channel-dialog"
import {
  alertChannelFormError,
  alertChannelFormFrom,
  emptyAlertChannelForm,
  serializeAlertChannelConfig,
} from "@/pages/admin/alert-channel-config"

function AlertChannelsPanel() {
  const { data: channels, loading, error, refresh } = useAlertChannels()
  const { locale, t } = useLocale()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<AlertChannel | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [testingId, setTestingId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [form, setForm] = useState(emptyAlertChannelForm)

  const openCreate = () => {
    setForm(emptyAlertChannelForm())
    setEditing(null)
    setActionError(null)
    setDialogOpen(true)
  }
  const openEdit = (channel: AlertChannel) => {
    setForm(alertChannelFormFrom(channel))
    setEditing(channel)
    setActionError(null)
    setDialogOpen(true)
  }
  const handleSave = async () => {
    const validationError = alertChannelFormError(form, editing !== null)
    if (validationError) {
      setActionError(t(validationError))
      return
    }
    setActionError(null)
    setActionLoading(true)
    const payload = {
      name: form.name.trim(),
      channel_type: form.channelType,
      config: serializeAlertChannelConfig(form),
      enabled: editing?.enabled ?? true,
    }
    try {
      if (editing) await api.updateAlertChannel(editing.id, payload)
      else await api.createAlertChannel(payload)
      await refresh()
      setDialogOpen(false)
      setEditing(null)
    } catch (caught) {
      setActionError(messageFromError(caught))
    } finally {
      setActionLoading(false)
    }
  }
  const handleToggle = async (channel: AlertChannel) => {
    setActionError(null)
    try {
      await api.updateAlertChannel(channel.id, {
        name: channel.name,
        channel_type: channel.channel_type,
        config: channel.config,
        enabled: !channel.enabled,
      })
      await refresh()
    } catch (caught) {
      setActionError(messageFromError(caught))
    }
  }
  const handleTest = async (channel: AlertChannel) => {
    setTestingId(channel.id)
    setActionError(null)
    try {
      await api.testAlertChannel(channel.id)
      await refresh()
    } catch (caught) {
      setActionError(messageFromError(caught))
    } finally {
      setTestingId(null)
    }
  }
  const handleDelete = async () => {
    if (!deleteId) return
    setActionLoading(true)
    setActionError(null)
    try {
      await api.deleteAlertChannel(deleteId)
      await refresh()
      setDeleteId(null)
    } catch (caught) {
      setActionError(messageFromError(caught))
    } finally {
      setActionLoading(false)
    }
  }

  if (error) return <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Button onClick={openCreate}><PlusIcon data-icon="inline-start" />{t("app.create")}</Button>
      </div>
      {actionError && <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>}
      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{t("node.name")}</TableHead>
            <TableHead>{t("app.type")}</TableHead>
            <TableHead>{t("alert.delivery")}</TableHead>
            <TableHead>{t("alert.enabled")}</TableHead>
            <TableHead className="w-52">{t("app.actions")}</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {loading ? <LoadingRows /> : channels?.length ? channels.map((channel) => (
              <TableRow key={channel.id}>
                <TableCell className="font-medium">{channel.name}</TableCell>
                <TableCell>{t(`alert.channel.${channel.channel_type}`)}</TableCell>
                <TableCell><DeliveryStatus channel={channel} locale={locale} /></TableCell>
                <TableCell><Switch checked={channel.enabled} onCheckedChange={() => handleToggle(channel)} /></TableCell>
                <TableCell><div className="flex items-center gap-1">
                  <Button variant="outline" size="sm" onClick={() => handleTest(channel)} disabled={testingId === channel.id}>
                    {testingId === channel.id
                      ? <LoaderCircleIcon data-icon="inline-start" className="animate-spin" />
                      : <SendIcon data-icon="inline-start" />}
                    {t("alert.test_send")}
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label={t("app.edit")} title={t("app.edit")} onClick={() => openEdit(channel)}><PencilIcon /></Button>
                  <Button variant="ghost" size="icon-sm" aria-label={t("app.delete")} title={t("app.delete")} onClick={() => setDeleteId(channel.id)}><Trash2Icon className="text-destructive" /></Button>
                </div></TableCell>
              </TableRow>
            )) : <TableRow><TableCell colSpan={5} className="text-center text-muted-foreground">{t("app.no_data")}</TableCell></TableRow>}
          </TableBody>
        </Table>
      </div>
      <AlertChannelDialog
        open={dialogOpen}
        editing={editing !== null}
        loading={actionLoading}
        form={form}
        setForm={setForm}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
      />
      <Dialog open={deleteId !== null} onOpenChange={(open) => { if (!open) setDeleteId(null) }}>
        <DialogContent><DialogHeader><DialogTitle>{t("confirm.delete")}</DialogTitle></DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteId(null)}>{t("app.cancel")}</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={actionLoading}>{t("app.delete")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function LoadingRows() {
  return Array.from({ length: 2 }).map((_, index) => (
    <TableRow key={index}>
      <TableCell><Skeleton className="h-4 w-20" /></TableCell>
      <TableCell><Skeleton className="h-4 w-16" /></TableCell>
      <TableCell><Skeleton className="h-5 w-24" /></TableCell>
      <TableCell><Skeleton className="h-4 w-10" /></TableCell>
      <TableCell><Skeleton className="h-8 w-40" /></TableCell>
    </TableRow>
  ))
}

function DeliveryStatus({ channel, locale }: { channel: AlertChannel; locale: string }) {
  const { t } = useLocale()
  if (!channel.last_delivery) return <span className="text-sm text-muted-foreground">{t("alert.never_delivered")}</span>
  const success = channel.last_delivery.state === "success"
  return <div className="flex flex-col items-start gap-1">
    <Badge variant={success ? "secondary" : "destructive"}>
      {t(success ? "alert.delivery_success" : "alert.delivery_failed")}
    </Badge>
    <span className="text-xs text-muted-foreground">
      {new Date(channel.last_delivery.delivered_at).toLocaleString(locale)}
    </span>
    {!success && (
      <span className="max-w-48 truncate text-xs text-muted-foreground" title={channel.last_delivery.message}>
        {channel.last_delivery.message}
      </span>
    )}
  </div>
}

export default AlertChannelsPanel
