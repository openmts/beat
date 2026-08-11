import { useMemo, useState } from "react"
import { ActivityIcon, PlusIcon } from "lucide-react"

import { NetworkHistoryDialog } from "@/components/network-history-dialog"
import { NetworkTaskCard } from "@/components/network-task-card"
import { NetworkTaskDialog } from "@/components/network-task-dialog"
import { PageHeader } from "@/components/page-header"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { useNodes } from "@/hooks/use-api"
import { useAdminNetworkTasks } from "@/hooks/use-network"
import * as networkApi from "@/lib/network-api"
import type { NetworkTaskPayload, NetworkTaskView } from "@/types"

export default function Network() {
  const { t } = useLocale()
  const tasks = useAdminNetworkTasks()
  const nodes = useNodes()
  const [editView, setEditView] = useState<NetworkTaskView | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteView, setDeleteView] = useState<NetworkTaskView | null>(null)
  const [historyView, setHistoryView] = useState<NetworkTaskView | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const ordered = useMemo(
    () => [...(tasks.data ?? [])].sort((a, b) => a.task.sort_order - b.task.sort_order),
    [tasks.data],
  )
  const enabled = ordered.filter((view) => view.task.enabled)
  const disabled = ordered.filter((view) => !view.task.enabled)

  const save = async (payload: NetworkTaskPayload) => runAction(async () => {
    if (editView) await networkApi.updateNetworkTask(editView.task.id, payload)
    else await networkApi.createNetworkTask(payload)
    await tasks.refresh()
    setDialogOpen(false)
  })

  const remove = async () => runAction(async () => {
    if (!deleteView) return
    await networkApi.deleteNetworkTask(deleteView.task.id)
    await tasks.refresh()
    setDeleteView(null)
  })

  const move = async (view: NetworkTaskView, direction: -1 | 1) => runAction(async () => {
    const section = view.task.enabled ? enabled : disabled
    const sectionIndex = section.findIndex((item) => item.task.id === view.task.id)
    const adjacent = section[sectionIndex + direction]
    if (!adjacent) return
    const ids = ordered.map((item) => item.task.id)
    const currentIndex = ids.indexOf(view.task.id)
    const adjacentIndex = ids.indexOf(adjacent.task.id)
    ;[ids[currentIndex], ids[adjacentIndex]] = [ids[adjacentIndex], ids[currentIndex]]
    await networkApi.sortNetworkTasks(ids)
    await tasks.refresh()
  })

  const runAction = async (action: () => Promise<void>) => {
    setActionError(null)
    setActionLoading(true)
    try {
      await action()
    } catch (error) {
      setActionError(error instanceof Error ? error.message : t("app.error"))
    } finally {
      setActionLoading(false)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t("network.manage")} description={t("network.manage_description")}>
        <Button onClick={() => { setEditView(null); setDialogOpen(true) }}>
          <PlusIcon data-icon="inline-start" />{t("network.create_task")}
        </Button>
      </PageHeader>
      {(tasks.error || nodes.error || actionError) && (
        <Alert variant="destructive"><AlertDescription>{tasks.error ?? nodes.error ?? actionError}</AlertDescription></Alert>
      )}
      {tasks.loading || nodes.loading ? (
        <TaskGridSkeleton />
      ) : ordered.length === 0 ? (
        <div className="flex min-h-48 flex-col items-center justify-center gap-2 border border-dashed text-center">
          <ActivityIcon className="size-5 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">{t("network.no_tasks")}</p>
        </div>
      ) : (
        <div className="flex flex-col gap-8">
          <TaskSection title={t("network.enabled_tasks")} views={enabled} onEdit={openEdit} onDelete={setDeleteView} onHistory={setHistoryView} onMove={move} />
          <TaskSection title={t("network.disabled_tasks")} views={disabled} onEdit={openEdit} onDelete={setDeleteView} onHistory={setHistoryView} onMove={move} />
        </div>
      )}
      <NetworkTaskDialog
        open={dialogOpen}
        view={editView}
        nodes={nodes.data ?? []}
        loading={actionLoading}
        error={actionError}
        nextSortOrder={ordered.length}
        onOpenChange={setDialogOpen}
        onSave={save}
      />
      <DeleteTaskDialog view={deleteView} loading={actionLoading} onOpenChange={(open) => { if (!open) setDeleteView(null) }} onDelete={remove} />
      <NetworkHistoryDialog open={historyView !== null} view={historyView} admin onOpenChange={(open) => { if (!open) setHistoryView(null) }} />
    </div>
  )

  function openEdit(view: NetworkTaskView) {
    setEditView(view)
    setDialogOpen(true)
  }
}

interface TaskSectionProps {
  title: string
  views: NetworkTaskView[]
  onEdit: (view: NetworkTaskView) => void
  onDelete: (view: NetworkTaskView) => void
  onHistory: (view: NetworkTaskView) => void
  onMove: (view: NetworkTaskView, direction: -1 | 1) => Promise<void>
}

function TaskSection({ title, views, onEdit, onDelete, onHistory, onMove }: TaskSectionProps) {
  if (views.length === 0) return null
  return (
    <section aria-labelledby={`network-section-${views[0].task.enabled}`} className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h3 id={`network-section-${views[0].task.enabled}`} className="text-base font-semibold">{title}</h3>
        <Badge variant="outline">{views.length}</Badge>
      </div>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
        {views.map((view, index) => (
          <NetworkTaskCard
            key={view.task.id}
            view={view}
            first={index === 0}
            last={index === views.length - 1}
            onEdit={() => onEdit(view)}
            onDelete={() => onDelete(view)}
            onHistory={() => onHistory(view)}
            onMoveUp={() => void onMove(view, -1)}
            onMoveDown={() => void onMove(view, 1)}
          />
        ))}
      </div>
    </section>
  )
}

function TaskGridSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-2 2xl:grid-cols-3">
      {Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-64" />)}
    </div>
  )
}

function DeleteTaskDialog({
  view,
  loading,
  onOpenChange,
  onDelete,
}: {
  view: NetworkTaskView | null
  loading: boolean
  onOpenChange: (open: boolean) => void
  onDelete: () => Promise<void>
}) {
  const { t } = useLocale()
  return (
    <Dialog open={view !== null} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader><DialogTitle>{t("confirm.delete")}</DialogTitle></DialogHeader>
        <p className="text-sm text-muted-foreground">{view?.task.name}</p>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button variant="destructive" disabled={loading} onClick={() => void onDelete()}>{t("app.delete")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
