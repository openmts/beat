import { Link } from "react-router"

import { useAuth } from "@/context/auth"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { OperationConfirm } from "./operation-confirm"
import { APIError } from "@/lib/api-error"
import { useMemo, useState } from "react"
import { useManagedNodes, useGroups, useSSHKeys } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { buildNodeSections, getSSHKeyName, toEditNode } from "@/lib/admin-node-groups"
import { AdminNodeCard, AdminNodeCardSkeleton } from "@/components/admin-node-card"
import { NodeEditDialog } from "@/components/node-edit-dialog"
import { TrafficCalibrationDialog } from "@/components/traffic-calibration-dialog"
import { TrafficCalibrationHistory } from "@/components/traffic-calibration-history"
import { EntityDeleteDialog } from "@/pages/admin/entity-delete-dialog"
import { createTrafficCalibration, newCalibrationRequestID, revokeTrafficCalibration, type TrafficCalibrationPayload } from "@/lib/traffic-calibration"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Alert, AlertDescription } from "@/components/ui/alert"
import type { EditableNode, NodeUpdatePayload } from "@/lib/node-traffic"
import type { AgentConfig, ManagedNode, NodeCredential } from "@/types"
import { PlusIcon } from "lucide-react"
import { PageHeader } from "@/components/page-header"
import {
  AgentConfigDialog,
  CreateNodeDialog,
} from "@/components/node-agent-dialogs"

function Nodes() {
 const { principal } = useAuth()
 return <NodesSession key={`${principal?.user.id}/${principal?.session.id}`} />
}

function NodesSession() {
  const { data: nodes, loading, error, refresh } = useManagedNodes()
  const { data: groups, loading: groupsLoading, error: groupsError } = useGroups()
  const { data: sshKeys, error: sshKeysError } = useSSHKeys()
  const { t } = useLocale()
  const flow = useConfirmedOperation()
  const [editNode, setEditNode] = useState<EditableNode | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ManagedNode | null>(null)
  const [actionMessage, setActionMessage] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [search, setSearch] = useState("")
  const [actionError, setActionError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [credential, setCredential] = useState<NodeCredential | AgentConfig | null>(null)
  const [calibrationNode, setCalibrationNode] = useState<ManagedNode | null>(null)
  const [historyNode, setHistoryNode] = useState<ManagedNode | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)

  const refreshAfterMutation = async () => {
    try { await refresh() }
    catch { setActionError(t("node.refresh_failed")) }
  }

  const handleUpdate = async (payload: NodeUpdatePayload) => {
    if (!editNode) return
    const target = editNode.id
    const sensitive = nodes?.find(node => node.id === target)?.ssh_public_key !== payload.ssh_public_key
    const confirmKeyUpdate = (forceProof = false) => {
      setEditNode(null)
      flow.request({ forceProof, title: `${t("app.edit")} · ${t("node.ssh_key")}`, description: t("node.ssh_key_description"), execute: async () => { await api.updateNode(target, payload); await refreshAfterMutation() } })
    }
    if (sensitive) { confirmKeyUpdate(); return }
    setActionError(null)
    setActionLoading(true)
    try {
      await api.updateNode(target, payload)
      await refreshAfterMutation()
      setEditNode(null)
    } catch (err) {
      if (err instanceof APIError && err.status === 428) confirmKeyUpdate(true)
      else setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleDelete = async (requestId: string) => {
    if (!deleteTarget) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.deleteNode(deleteTarget.id, deleteTarget.name, requestId)
      setActionMessage(t("node.deletion_queued"))
      setDeleteTarget(null)
      await refreshAfterMutation()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleCreate = async (payload: Parameters<typeof api.createManagedNode>[0]) => {
    setCreateOpen(false)
    flow.request({ title: `${t("agent.create_node")} · ${payload.name}`, description: t("agent.create_credential_description"), execute: async () => {
      setCredential(await api.createManagedNode(payload)); await refreshAfterMutation()
    } })
  }

  const handleAgentAction = (node: ManagedNode, type: "rotate" | "revoke") => {
    flow.request({ title: type === "rotate" ? t("agent.rotate_confirm") : t("agent.revoke_confirm"), description: t("agent.credential_revoke_description"), execute: async () => {
      if (type === "rotate") setCredential(await api.rotateAgentToken(node.id, window.location.origin))
      else await api.revokeAgentToken(node.id)
      await refreshAfterMutation()
    } })
  }

  const handleCalibrate = (payload: TrafficCalibrationPayload) => {
    const node = calibrationNode
    if (!node) return
    setCalibrationNode(null)
    flow.request({
      title: `${t("traffic.calibrate")} · ${node.name}`,
      description: t("traffic.calibration_confirm_description"),
      execute: async () => {
        await createTrafficCalibration(node.id, payload)
        await refreshAfterMutation()
      },
    })
  }

  const handleRevokeCalibration = (node: ManagedNode, eventId: string) => {
    flow.request({
      title: `${t("traffic.calibration_revoke_confirm_title")} · ${node.name}`,
      description: t("traffic.calibration_revoke_confirm_description"),
      execute: async () => {
        await revokeTrafficCalibration(node.id, eventId, newCalibrationRequestID())
        setHistoryOpen(false)
        await refreshAfterMutation()
      },
    })
  }

  const handleInstall = async (node: ManagedNode) => {
    setActionError(null)
    try {
      setCredential(await api.getAgentInstallConfig(node.id, window.location.origin))
    } catch (err) {
      setActionError(messageFromError(err))
    }
  }

  const handleMove = async (nodeIds: string[], index: number, offset: -1 | 1) => {
    const target = index + offset
    if (target < 0 || target >= nodeIds.length) return
    const reordered = [...nodeIds]
    ;[reordered[index], reordered[target]] = [reordered[target], reordered[index]]
    setActionError(null)
    setActionLoading(true)
    try {
      await api.updateNodeSort(reordered)
      await refreshAfterMutation()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const groupOptions = useMemo(() => groups?.map((group) => ({
    label: group.name,
    value: group.id,
  })) ?? [], [groups])
  const sshKeyNames = useMemo(
    () => new Map(sshKeys?.map((key) => [key.public_key, key.name]) ?? []),
    [sshKeys],
  )
  const nodeSections = useMemo(
    () => buildNodeSections({
      nodes: nodes ?? [],
      groups: groups ?? [],
      search,
      unassignedName: t("node.not_assigned"),
    }),
    [groups, nodes, search, t],
  )
  const pageLoading = loading || groupsLoading

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("app.nodes")} description={t("node.manage_description")}>
        <div className="flex w-full flex-wrap gap-2 sm:w-auto">
          <Input
            className="min-w-0 flex-1 sm:w-72"
            placeholder={t("app.search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Button onClick={() => setCreateOpen(true)} disabled={flow.busy}>
            <PlusIcon />{t("agent.create_node")}
          </Button>
        </div>
      </PageHeader>
      {actionError && !deleteTarget && (
        <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>
      )}
      {actionMessage && (
        <Alert>
          <AlertDescription>
            {actionMessage}{" "}
            <Link className="underline underline-offset-4" to="/admin/settings">{t("storage.tab_erase")}</Link>
          </AlertDescription>
        </Alert>
      )}
      {sshKeysError && (
        <Alert variant="destructive"><AlertDescription>{sshKeysError}</AlertDescription></Alert>
      )}
      {groupsError && (
        <Alert variant="destructive"><AlertDescription>{groupsError}</AlertDescription></Alert>
      )}

      {pageLoading ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          {Array.from({ length: 6 }).map((_, index) => (
            <AdminNodeCardSkeleton key={index} />
          ))}
        </div>
      ) : nodeSections.length > 0 ? (
        <div className="flex flex-col gap-6">
          {nodeSections.map((section) => (
            <section
              key={section.id}
              aria-labelledby={`node-group-${section.id}`}
              className="flex flex-col gap-3 border-t pt-5 first:border-t-0 first:pt-0"
            >
              <div className="flex items-center gap-2">
                <h3 id={`node-group-${section.id}`} className="text-base font-semibold">
                  {section.name}
                </h3>
                <Badge variant="outline">{section.nodes.length}</Badge>
              </div>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                {section.nodes.map((node, index) => (
                  <AdminNodeCard
                    key={node.id}
                    node={node}
                    sshKeyName={getSSHKeyName(node, sshKeyNames, {
                      notAssigned: t("node.not_assigned"),
                      unknown: t("node.unknown_key"),
                    })}
                    onEdit={() => setEditNode(toEditNode(node))}
                    onDelete={() => setDeleteTarget(node)}
                    onInstall={() => void handleInstall(node)}
                    onRotate={() => handleAgentAction(node, "rotate")}
                    onRevoke={() => handleAgentAction(node, "revoke")}
                    onCalibrate={() => setCalibrationNode(node)}
                    canMoveUp={index > 0 && !actionLoading}
                    canMoveDown={index < section.nodes.length - 1 && !actionLoading}
                    onMoveUp={() => void handleMove(section.nodes.map((item) => item.id), index, -1)}
                    onMoveDown={() => void handleMove(section.nodes.map((item) => item.id), index, 1)}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      ) : (
        <div className="flex min-h-40 items-center justify-center text-sm text-muted-foreground">
          {t("app.no_data")}
        </div>
      )}

      <NodeEditDialog
        node={editNode}
        groups={groupOptions}
        sshKeys={sshKeys ?? []}
        loading={actionLoading || flow.busy}
        onChange={setEditNode}
        onSave={handleUpdate}
      />

      <CreateNodeDialog
        groups={groupOptions}
        loading={actionLoading || flow.busy}
        open={createOpen}
        sshKeys={sshKeys ?? []}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
      />

      <AgentConfigDialog value={credential} onClose={() => setCredential(null)} />

      <OperationConfirm flow={flow} />

      <TrafficCalibrationDialog
        node={calibrationNode}
        open={Boolean(calibrationNode)}
        pending={flow.busy}
        onOpenChange={(open) => { if (!open) setCalibrationNode(null) }}
        onSubmit={handleCalibrate}
        onOpenHistory={() => {
          setHistoryNode(calibrationNode)
          setCalibrationNode(null)
          setHistoryOpen(true)
        }}
      />

      <TrafficCalibrationHistory
        nodeId={historyNode?.id ?? null}
        open={historyOpen}
        pending={flow.busy}
        onOpenChange={(open) => { setHistoryOpen(open); if (!open) setHistoryNode(null) }}
        onRevoke={(eventId) => { if (historyNode) handleRevokeCalibration(historyNode, eventId) }}
      />

      <EntityDeleteDialog
        target={deleteTarget ? { id: deleteTarget.id, name: deleteTarget.name, scope: "delete_node" } : null}
        pending={actionLoading}
        error={actionError}
        onOpenChange={(open) => { if (!open) { setDeleteTarget(null); setActionError(null) } }}
        onConfirm={handleDelete}
      />
    </div>
  )
}

export default Nodes

function messageFromError(err: unknown) {
  return err instanceof Error ? err.message : "The request failed"
}
