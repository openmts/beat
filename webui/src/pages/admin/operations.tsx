import { useCallback, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { useAuth } from "@/context/auth"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { OperationTransfers } from "./operation-transfers"
import { OperationPolicies } from "./operation-policies"
import { OperationSnippets, type OperationSnippetDraft } from "./operation-snippets"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { getOperationTasks, getOperationServerStatus } from "@/lib/operations-api"
import { OperationCreate } from "./operation-create"
import { OperationTaskDetail } from "./operation-task-detail"
import { OperationStatus } from "./operation-status"
import { OperationRuntime } from "./operation-runtime"
import type { Translate } from "@/context/locales"
// 任务类型是后端契约里的枚举；界面只负责把它翻成可读名字，未知类型回落原值。
const taskKindKeys: Record<string, string> = {
 shell: "operations.task_kind_shell", snippet: "operations.task_kind_snippet",
 deploy: "operations.task_kind_deploy", collect: "operations.task_kind_collect",
}
export function taskKindLabel(kind: string, t: Translate) { return taskKindKeys[kind] ? t(taskKindKeys[kind]) : kind }
import { RuntimeDiagnosticsPanel } from "./runtime-diagnostics-panel"
import { MigrationRecoveryPanel } from "./migration-recovery-panel"
import { IngressGovernancePanel } from "./ingress-governance-panel"

export default function Operations() {
 const { t } = useLocale()
 const { principal } = useAuth()
 const status = useOperationResource(getOperationServerStatus)
 if (status.data?.enabled) return <><OwnerRuntime principal={principal} /><OperationWorkspace /></>
 return <div className="space-y-6"><OwnerRuntime principal={principal} /><Card><CardContent className="space-y-4 pt-6"><h1 className="text-xl font-semibold">{t("operations.remote_operations")}</h1>
  {status.error ? <p role="alert" className="text-destructive">{status.error}</p> : <p>{status.loading ? (t("operations.loading_server_status")) : (t("operations.remote_operations_are_disabled_on_the_server_the"))}</p>}
  <Button variant="outline" onClick={status.reload}>{t("operations.check_again")}</Button>
 </CardContent></Card></div>
}

function OwnerRuntime({ principal }: { principal: ReturnType<typeof useAuth>["principal"] }) {
 // 运行状态接口只对 owner 开放，其他管理员看到原有远程操作视图。
 return principal?.user.role === "owner" ? <OperationRuntime /> : null
}

function OperationWorkspace() {
 const { locale, t } = useLocale()
 const [cursor, setCursor] = useState(""), [history, setHistory] = useState<string[]>([]), [selected, setSelected] = useState("")
 const [tab, setTab] = useState("tasks")
 const [creating, setCreating] = useState(false)
 const [preset, setPreset] = useState<OperationSnippetDraft | undefined>()
 const { principal } = useAuth()
 const resource = useOperationResource(useCallback(signal => getOperationTasks(cursor, signal), [cursor]), 5000)
 const select = (id: string) => { setSelected(id); setTab("tasks"); setCreating(false); setPreset(undefined); resource.reload() }
 return <div className="space-y-6"><header className="flex flex-wrap items-start justify-between gap-3"><div><h1 className="text-2xl font-semibold">{t("operations.remote_operations")}</h1><p className="mt-1 text-sm text-muted-foreground">{t("operations.dispatch_controlled_agent_tasks_and_track_execut")}</p></div><div className="flex gap-2"><Button variant="outline" onClick={resource.reload}>{t("operations.refresh")}</Button><Button onClick={() => { setPreset(undefined); setCreating(true) }}>{t("operations.create_task")}</Button></div></header>
  <Tabs value={tab} onValueChange={value => setTab(String(value))}><TabsList><TabsTrigger value="tasks">{t("operations.tasks")}</TabsTrigger><TabsTrigger value="files">{t("operations.files")}</TabsTrigger><TabsTrigger value="snippets">{t("operations.snippets")}</TabsTrigger>{principal?.user.role === "owner" && <TabsTrigger value="policies">{t("operations.policies")}</TabsTrigger>}{principal?.user.role === "owner" && <TabsTrigger value="diagnostics">{t("operations.runtime_diagnostics")}</TabsTrigger>}{principal?.user.role === "owner" && <TabsTrigger value="migration">{t("operations.migration_and_recovery")}</TabsTrigger>}{principal?.user.role === "owner" && <TabsTrigger value="ingress">{t("operations.ingress_admission")}</TabsTrigger>}</TabsList><TabsContent value="tasks" className="space-y-4">
  {resource.error && <p role="alert" className="rounded-md border border-destructive/30 p-3 text-sm text-destructive">{resource.error}</p>}
  <Card><CardContent className="pt-4"><div className="overflow-auto"><table className="w-full text-left text-sm"><thead className="text-muted-foreground"><tr><th className="p-3">{t("operations.created")}</th><th className="p-3">{t("operations.kind")}</th><th className="p-3">{t("operations.state")}</th><th className="p-3">{t("operations.targets")}</th><th className="p-3">{t("operations.actions")}</th></tr></thead><tbody>{resource.data?.items.map(task => <tr key={task.id} className="border-t"><td className="p-3">{formatDateTime(locale, task.created_at)}</td><td className="p-3">{taskKindLabel(task.kind, t)}</td><td className="p-3"><OperationStatus state={task.state ?? "queued"} /></td><td className="p-3">{t("operations.succeeded")} {task.succeeded_count ?? 0} / {task.target_count ?? 0} · {t("operations.failed")} {task.failed_count ?? 0}</td><td className="p-3"><Button size="sm" variant="outline" onClick={() => setSelected(task.id)}>{t("operations.details")}</Button></td></tr>)}</tbody></table></div>
   {!resource.data?.items.length && <p className="p-8 text-center text-sm text-muted-foreground">{resource.loading ? (t("operations.loading")) : (t("operations.no_remote_tasks"))}</p>}
   <div className="mt-3 flex justify-end gap-2"><Button size="sm" variant="outline" disabled={!history.length} onClick={() => { setCursor(history.at(-1) ?? ""); setHistory(value => value.slice(0, -1)) }}>{t("operations.previous")}</Button><Button size="sm" variant="outline" disabled={!resource.data?.next_cursor || resource.loading} onClick={() => { setHistory(value => [...value, cursor]); setCursor(resource.data?.next_cursor ?? "") }}>{t("operations.next")}</Button></div>
  </CardContent></Card>
  {selected && <OperationTaskDetail key={selected} id={selected} close={() => setSelected("")} />}
  </TabsContent><TabsContent value="files"><OperationTransfers task={select} /></TabsContent><TabsContent value="snippets"><OperationSnippets execute={value => { setPreset(value); setCreating(true) }} /></TabsContent>{principal?.user.role === "owner" && <TabsContent value="policies"><OperationPolicies /></TabsContent>}{principal?.user.role === "owner" && <TabsContent value="diagnostics"><RuntimeDiagnosticsPanel /></TabsContent>}{principal?.user.role === "owner" && <TabsContent value="migration"><MigrationRecoveryPanel /></TabsContent>}{principal?.user.role === "owner" && <TabsContent value="ingress"><IngressGovernancePanel /></TabsContent>}</Tabs>
  {creating && <OperationCreate preset={preset} close={() => { setCreating(false); setPreset(undefined) }} created={select} />}
 </div>
}
