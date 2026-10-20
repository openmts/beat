import { useEffect, useEffectEvent, useState } from "react"
import { useSearchParams } from "react-router"
import { useAuth } from "@/context/auth"
import { useWorkspaceEditor } from "@/hooks/use-workspace-editor"
import type { WorkspaceSources } from "@/lib/metric-workspace-api"
import { workspaceAllSelection } from "@/lib/workspace-variable-options"
import { workspaceVariableSearch, workspaceVariableSelections } from "@/lib/workspace-url-state"
import type { DashboardPanel, WorkspaceRange, WorkspaceSelections } from "@/types/metric-workspace"
import type { WorkspaceConfirmation } from "@/components/metric-workspace/confirm"

export function useWorkspacePage(sources: WorkspaceSources) {
  const { principal } = useAuth()
  const [parameters, setParameters] = useSearchParams()
  const editor = useWorkspaceEditor(sources.dashboards, parameters.get("dashboard") ?? undefined)
  const [range, setRange] = useState<WorkspaceRange>({ mode: "relative", seconds: editor.record.definition.time_range_seconds })
  // 变量选择可以从地址栏恢复：分享链接与刷新都必须保留同一组筛选。
  const [values, setValues] = useState<WorkspaceSelections>(() => workspaceVariableSelections(editor.record.definition.variables, parameters))
  const [panel, setPanel] = useState<DashboardPanel | null>(null)
  const [alertPanel, setAlertPanel] = useState<DashboardPanel | null>(null)
  const [libraryPanel, setLibraryPanel] = useState<DashboardPanel | null>(null)
  const [annotationReload, setAnnotationReload] = useState(0)
  const [snapshots, setSnapshots] = useState(false)
  const [versions, setVersions] = useState(false)
  const [duplicateOpen, setDuplicateOpen] = useState(false)
  const [pastePanel, setPastePanel] = useState(false)
  const [libraryOpen, setLibraryOpen] = useState(false)
  const [inspecting, setInspecting] = useState<string | null>(null)
  const [details, setDetails] = useState(false)
  const [confirmation, setConfirmation] = useState<WorkspaceConfirmation | null>(null)
  const [reauth, setReauth] = useState<{ action: () => Promise<void> } | null>(null)
  const [proofUntil, setProofUntil] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [link, setLink] = useState<string | null>(null)
  const variableKey = JSON.stringify(editor.record.definition.variables)
  const nodeParameter = parameters.get("node_id")
  const reconcileVariables = useEffectEvent(() => {
    setValues((previous) => Object.fromEntries(editor.record.definition.variables.map((variable) => {
      const choices = variable.type === "node" ? sources.nodes : sources.groups
      const available = new Set(choices.map((choice) => choice.id))
      // URL 指定的节点优先；其余保留仍然有效的选择，全部失效时回落到第一个选项。
      const preferred = variable.type === "node" && nodeParameter ? [nodeParameter] : previous[variable.name] ?? []
      const kept = preferred.filter((id) => (id === workspaceAllSelection && variable.include_all === true) ? choices.length > 0 : available.has(id))
      const fallback = choices[0]?.id
      return [variable.name, kept.length > 0 ? kept : fallback ? [fallback] : []]
    })))
  })
  useEffect(() => { reconcileVariables() }, [variableKey, sources.nodes, sources.groups, nodeParameter])
  // 选择变化就写回地址栏；只有查询串真的不同才导航，避免等价历史记录堆叠。
  useEffect(() => {
    setParameters((previous) => {
      const next = workspaceVariableSearch(editor.record.definition.variables, values, previous)
      return next.toString() === previous.toString() ? previous : next
    }, { replace: true })
  }, [values, editor.record.definition.variables, setParameters])
  const resetRange = useEffectEvent(() => { setRange({ mode: "relative", seconds: editor.record.definition.time_range_seconds }) })
  useEffect(() => { resetRange() }, [editor.record.id])
  useEffect(() => {
    if (!editor.dirty) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = "" }
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [editor.dirty])
  return { editor, range, setRange, values, setValues, panel, setPanel, alertPanel, setAlertPanel,
    libraryPanel, setLibraryPanel, annotationReload, setAnnotationReload, snapshots, setSnapshots, versions, setVersions, libraryOpen, setLibraryOpen,
    duplicateOpen, setDuplicateOpen, pastePanel, setPastePanel,
    inspecting, setInspecting, details, setDetails,
    confirmation, setConfirmation, reauth, setReauth, proofUntil: proofUntil ?? principal?.session.reauthenticated_until ?? null,
    setProofUntil, error, setError, link, setLink, owner: principal?.user.role === "owner", totp: !!principal?.user.totp_enabled_at }
}

export type WorkspacePage = ReturnType<typeof useWorkspacePage>
