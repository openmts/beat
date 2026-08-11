import { useEffect, useState } from "react"

import { NetworkTaskFields } from "@/components/network-task-fields"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { FieldGroup } from "@/components/ui/field"
import { useLocale } from "@/context/locale"
import type { NetworkTaskPayload, NetworkTaskView, Node } from "@/types"

interface NetworkTaskDialogProps {
  open: boolean
  view: NetworkTaskView | null
  nodes: Node[]
  loading: boolean
  error: string | null
  nextSortOrder: number
  onOpenChange: (open: boolean) => void
  onSave: (payload: NetworkTaskPayload) => Promise<void>
}

const emptyPayload: NetworkTaskPayload = {
  name: "",
  type: "icmp",
  target: "",
  ip_family: "auto",
  interval_seconds: 60,
  timeout_milliseconds: 3000,
  all_nodes: true,
  enabled: true,
  is_public: true,
  sort_order: 0,
  node_ids: [],
}

export function NetworkTaskDialog({
  open,
  view,
  nodes,
  loading,
  error,
  nextSortOrder,
  onOpenChange,
  onSave,
}: NetworkTaskDialogProps) {
  const { t } = useLocale()
  const [payload, setPayload] = useState<NetworkTaskPayload>(emptyPayload)
  const [search, setSearch] = useState("")
  const [validationError, setValidationError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setPayload(view ? payloadFromView(view) : { ...emptyPayload, sort_order: nextSortOrder })
    setSearch("")
    setValidationError(null)
  }, [nextSortOrder, open, view])

  const save = async () => {
    const message = validatePayload(payload, t)
    if (message) {
      setValidationError(message)
      return
    }
    setValidationError(null)
    await onSave(payload)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{view ? t("network.edit_task") : t("network.create_task")}</DialogTitle>
          <DialogDescription>{t("network.task_description")}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          {(error || validationError) && (
            <Alert variant="destructive"><AlertDescription>{validationError ?? error}</AlertDescription></Alert>
          )}
          <NetworkTaskFields
            payload={payload}
            nodes={nodes}
            search={search}
            t={t}
            onSearchChange={setSearch}
            onChange={(changes) => setPayload((current) => ({ ...current, ...changes }))}
          />
        </FieldGroup>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button onClick={() => void save()} disabled={loading}>{t("app.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function payloadFromView(view: NetworkTaskView): NetworkTaskPayload {
  const task = view.task
  return {
    name: task.name,
    type: task.type,
    target: task.target,
    ip_family: task.ip_family,
    interval_seconds: task.interval_seconds,
    timeout_milliseconds: task.timeout_milliseconds,
    all_nodes: task.all_nodes,
    enabled: task.enabled,
    is_public: task.is_public,
    sort_order: task.sort_order,
    node_ids: task.nodes.map((node) => node.id),
  }
}

function validatePayload(payload: NetworkTaskPayload, t: (key: string) => string): string | null {
  if (!payload.name.trim() || !payload.target.trim()) return t("network.validation_required")
  if (payload.interval_seconds < 10 || payload.interval_seconds > 86400) return t("network.validation_interval")
  if (payload.timeout_milliseconds < 100 || payload.timeout_milliseconds > 30000) return t("network.validation_timeout")
  if (payload.timeout_milliseconds > payload.interval_seconds * 1000) return t("network.validation_timeout_interval")
  return null
}
