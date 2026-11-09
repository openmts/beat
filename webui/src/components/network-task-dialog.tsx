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
  direction: "agent_to_target",
  type: "icmp",
  target_mode: "single",
  target: "",
  targets: [],
  ip_family: "auto",
  interval_seconds: 60,
  timeout_milliseconds: 3000,
  packet_count: 5,
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
    const normalized = normalizePayload(payload, t)
    const message = validatePayload(normalized, t)
    if (message) {
      setValidationError(message)
      return
    }
    setValidationError(null)
    await onSave(normalized)
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

function normalizePayload(payload: NetworkTaskPayload, t: (key: string) => string): NetworkTaskPayload {
  if (payload.direction === "server_to_agent") {
    return { ...payload, target_mode: "single", targets: [] }
  }
  if (payload.target_mode === "single") {
    const current = payload.targets[0]
    return {
      ...payload,
      targets: [{
        id: current?.id ?? "",
        label: current?.label || payload.name || t("network.target_mode.single"),
        carrier: current?.carrier ?? "custom",
        region: current?.region ?? "",
        target: payload.target,
        ip_family: current?.ip_family ?? payload.ip_family,
        packet_count: current?.packet_count ?? payload.packet_count,
        visible_address: current?.visible_address ?? false,
        enabled: true,
        sort_order: 0,
      }],
    }
  }
  return { ...payload, targets: payload.targets.map((target, index) => ({ ...target, sort_order: index })) }
}

function payloadFromView(view: NetworkTaskView): NetworkTaskPayload {
  const task = view.task
  return {
    name: task.name,
    direction: task.direction ?? "agent_to_target",
    type: task.type,
    target_mode: task.target_mode ?? "single",
    target: task.target,
    targets: task.targets ?? [],
    ip_family: task.ip_family,
    interval_seconds: task.interval_seconds,
    timeout_milliseconds: task.timeout_milliseconds,
    packet_count: task.packet_count ?? (task.type === "icmp" ? 5 : 1),
    all_nodes: task.all_nodes,
    enabled: task.enabled,
    is_public: task.is_public,
    sort_order: task.sort_order,
    node_ids: task.nodes.map((node) => node.id),
  }
}

function validatePayload(payload: NetworkTaskPayload, t: (key: string) => string): string | null {
  if (!payload.name.trim()) return t("network.validation_required")
  if (payload.direction === "server_to_agent" && !payload.target.trim()) return t("network.validation_required")
  if (payload.direction === "agent_to_target" && payload.target_mode === "single" && !payload.target.trim()) {
    return t("network.validation_required")
  }
  if (payload.direction === "agent_to_target" && payload.target_mode === "group" &&
    (payload.targets.length === 0 || payload.targets.some((target) => !target.target.trim()))) {
    return t("network.validation_targets")
  }
  if ((payload.type === "icmp" || payload.direction === "agent_to_target") &&
    (!Number.isInteger(payload.packet_count) || payload.packet_count < 1 || payload.packet_count > 20)) {
    return t("network.validation_packet_count")
  }
  if (payload.direction === "server_to_agent" && !validServerTarget(payload.type, payload.target)) {
    return t("network.validation_server_target")
  }
  if (payload.interval_seconds < 10 || payload.interval_seconds > 86400) return t("network.validation_interval")
  if (payload.timeout_milliseconds < 100 || payload.timeout_milliseconds > 30000) return t("network.validation_timeout")
  if (payload.timeout_milliseconds > payload.interval_seconds * 1000) return t("network.validation_timeout_interval")
  return null
}

function validServerTarget(type: NetworkTaskPayload["type"], target: string): boolean {
  if (type === "icmp") return target === "{{host}}"
  if (type === "tcp") return target === "{{host_port}}"
  if (type !== "http" || (!target.includes("{{host}}") && !target.includes("{{host_port}}"))) return false
  return !/\{\{/.test(target.replaceAll("{{host}}", "").replaceAll("{{host_port}}", "").replaceAll("{{ssh_port}}", "")) &&
    !/\}\}/.test(target.replaceAll("{{host}}", "").replaceAll("{{host_port}}", "").replaceAll("{{ssh_port}}", ""))
}
