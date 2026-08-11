import { useMemo, useState } from "react"
import { useManagedNodes, useGroups, useSSHKeys } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { buildNodeSections, getSSHKeyName, toEditNode } from "@/lib/admin-node-groups"
import { AdminNodeCard, AdminNodeCardSkeleton } from "@/components/admin-node-card"
import { NodeEditDialog } from "@/components/node-edit-dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog"
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
  const { data: nodes, loading, error, refresh } = useManagedNodes()
  const { data: groups, loading: groupsLoading, error: groupsError } = useGroups()
  const { data: sshKeys, error: sshKeysError } = useSSHKeys()
  const { t } = useLocale()
  const [editNode, setEditNode] = useState<EditableNode | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [search, setSearch] = useState("")
  const [actionError, setActionError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [credential, setCredential] = useState<NodeCredential | AgentConfig | null>(null)
  const [agentAction, setAgentAction] = useState<{
    node: ManagedNode
    type: "rotate" | "revoke"
  } | null>(null)

  const handleUpdate = async (payload: NodeUpdatePayload) => {
    if (!editNode) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.updateNode(editNode.id, payload)
      await refresh()
      setEditNode(null)
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
      await api.deleteNode(deleteId)
      await refresh()
      setDeleteId(null)
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleCreate = async (payload: Parameters<typeof api.createManagedNode>[0]) => {
    setActionError(null)
    setActionLoading(true)
    try {
      const result = await api.createManagedNode(payload)
      setCreateOpen(false)
      setCredential(result)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleAgentAction = async () => {
    if (!agentAction) return
    setActionError(null)
    setActionLoading(true)
    try {
      if (agentAction.type === "rotate") {
        setCredential(await api.rotateAgentToken(agentAction.node.id, window.location.origin))
      } else {
        await api.revokeAgentToken(agentAction.node.id)
      }
      setAgentAction(null)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
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
      await refresh()
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
        <div className="flex w-full gap-2 sm:w-auto">
          <Input
            className="min-w-0 flex-1 sm:w-72"
            placeholder={t("app.search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Button onClick={() => setCreateOpen(true)}>
            <PlusIcon />{t("agent.create_node")}
          </Button>
        </div>
      </PageHeader>
      {actionError && (
        <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>
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
                    onDelete={() => setDeleteId(node.id)}
                    onInstall={() => void handleInstall(node)}
                    onRotate={() => setAgentAction({ node, type: "rotate" })}
                    onRevoke={() => setAgentAction({ node, type: "revoke" })}
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
        loading={actionLoading}
        onChange={setEditNode}
        onSave={handleUpdate}
      />

      <CreateNodeDialog
        groups={groupOptions}
        loading={actionLoading}
        open={createOpen}
        sshKeys={sshKeys ?? []}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
      />

      <AgentConfigDialog value={credential} onClose={() => setCredential(null)} />

      <Dialog open={agentAction !== null} onOpenChange={(open) => { if (!open) setAgentAction(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {agentAction?.type === "rotate" ? t("agent.rotate_confirm") : t("agent.revoke_confirm")}
            </DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAgentAction(null)}>{t("app.cancel")}</Button>
            <Button
              variant={agentAction?.type === "revoke" ? "destructive" : "default"}
              onClick={handleAgentAction}
              disabled={actionLoading}
            >
              {agentAction?.type === "rotate" ? t("agent.rotate") : t("agent.revoke")}
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

export default Nodes

function messageFromError(err: unknown) {
  return err instanceof Error ? err.message : "The request failed"
}
