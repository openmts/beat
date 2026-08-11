import { useState } from "react"
import { useGroups } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { PlusIcon, PencilIcon, Trash2Icon, StarIcon, ArrowUpIcon, ArrowDownIcon } from "lucide-react"
import { PageHeader } from "@/components/page-header"

function Groups() {
  const { data: groups, loading, error, refresh } = useGroups()
  const { t } = useLocale()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingName, setEditingName] = useState("")
  const [editId, setEditId] = useState<string | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const handleCreate = async () => {
    if (!editingName.trim()) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.createGroup(editingName.trim())
      await refresh()
      setDialogOpen(false)
      setEditingName("")
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleRename = async () => {
    if (!editId || !editingName.trim()) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.updateGroup(editId, editingName.trim())
      await refresh()
      setEditId(null)
      setEditingName("")
      setDialogOpen(false)
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteId) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.deleteGroup(deleteId)
      await refresh()
      setDeleteId(null)
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleSetDefault = async (id: string) => {
    setActionError(null)
    try {
      await api.setDefaultGroup(id)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    }
  }

  const handleMoveUp = async (idx: number) => {
    if (!groups || idx <= 0) return
    const ids = groups.map((g) => g.id)
    ;[ids[idx], ids[idx - 1]] = [ids[idx - 1], ids[idx]]
    setActionError(null)
    try {
      await api.updateGroupSort(ids)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    }
  }

  const handleMoveDown = async (idx: number) => {
    if (!groups || idx >= groups.length - 1) return
    const ids = groups.map((g) => g.id)
    ;[ids[idx], ids[idx + 1]] = [ids[idx + 1], ids[idx]]
    setActionError(null)
    try {
      await api.updateGroupSort(ids)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    }
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("group.manage")}
        description={t("group.manage_description")}
      >
        <Button
          onClick={() => {
            setEditId(null)
            setEditingName("")
            setDialogOpen(true)
          }}
        >
          <PlusIcon data-icon="inline-start" />
          {t("app.create")}
        </Button>
      </PageHeader>
      {actionError && (
        <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>
      )}

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-16">#</TableHead>
                <TableHead>{t("group.name")}</TableHead>
                <TableHead className="w-24">{t("group.default")}</TableHead>
                <TableHead className="w-40">{t("app.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-8 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : groups && groups.length > 0 ? (
                groups.map((group, idx) => (
                  <TableRow key={group.id}>
                    <TableCell className="font-mono text-xs text-muted-foreground">{idx + 1}</TableCell>
                    <TableCell className="font-medium">{group.name}</TableCell>
                    <TableCell>
                      {group.is_default && <Badge variant="secondary">{t("group.default")}</Badge>}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleMoveUp(idx)}
                          disabled={idx === 0}
                        >
                          <ArrowUpIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleMoveDown(idx)}
                          disabled={idx === groups.length - 1}
                        >
                          <ArrowDownIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => {
                            setEditId(group.id)
                            setEditingName(group.name)
                            setDialogOpen(true)
                          }}
                        >
                          <PencilIcon />
                        </Button>
                        {!group.is_default && (
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => handleSetDefault(group.id)}
                          >
                            <StarIcon />
                          </Button>
                        )}
                        {!group.is_default && (
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => setDeleteId(group.id)}
                          >
                            <Trash2Icon className="text-destructive" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    {t("app.no_data")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editId ? t("app.edit") : t("app.create")} {t("group.name")}</DialogTitle>
          </DialogHeader>
          <Input
            value={editingName}
            onChange={(e) => setEditingName(e.target.value)}
            placeholder={t("group.name")}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>{t("app.cancel")}</Button>
            <Button onClick={editId ? handleRename : handleCreate} disabled={actionLoading || !editingName.trim()}>
              {t("app.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteId} onOpenChange={(v) => { if (!v) setDeleteId(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("confirm.delete")}</DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteId(null)}>{t("app.cancel")}</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={actionLoading}>
              {t("app.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export default Groups

function messageFromError(err: unknown) {
  return err instanceof Error ? err.message : "The request failed"
}
