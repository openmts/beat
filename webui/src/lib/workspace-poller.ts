import { retryAfterDelay } from "@/lib/retry-after"
import { queryWorkspaceMetrics } from "@/lib/metric-workspace-api"
import { dashboardDomain, expandPanelSeries, panelMetricQuery, panelQueryPoints, runWorkspaceQueue } from "@/lib/workspace-query"
import { workspacePanelInstances, type WorkspacePanelInstance } from "@/lib/workspace-repeat"
import { isStaticPanel } from "@/lib/workspace-row"
import { panelQueryDomain } from "@/lib/workspace-time-override"
import type { Group, Node } from "@/types"
import type { DashboardDefinition, PanelQueryState, WorkspaceRange, WorkspaceSelections } from "@/types/metric-workspace"

export interface WorkspacePollingOptions {
  dashboardId?: string
  definition: DashboardDefinition
  nodes: Node[]
  groups: Group[]
  selections: WorkspaceSelections
  range: WorkspaceRange
  public?: boolean
}

export type WorkspacePollEvent = { type: "range"; domain: { from: string; to: string }; dashboardId?: string; panelIds: string[] } |
  { type: "panel"; id: string; patch: Partial<PanelQueryState> } | { type: "complete" }

export class WorkspacePoller {
  private readonly options: WorkspacePollingOptions
  private readonly update: (event: WorkspacePollEvent) => void
  private stopped = false
  private readonly retries = new Map<string, AbortController>()
  private controller?: AbortController
  private timer?: ReturnType<typeof setTimeout>
  private retryDelay = 0
  private completed = false
  private readonly visibility = () => {
    if (document.hidden) { this.controller?.abort(); clearTimeout(this.timer) }
    else if (this.interval > 0 || !this.completed) void this.cycle()
  }

  constructor(options: WorkspacePollingOptions, update: (event: WorkspacePollEvent) => void) {
    this.options = options
    this.update = update
  }

  private get interval() { return this.options.range.mode === "relative" ? this.options.definition.refresh_seconds * 1000 : 0 }

  start(): () => void {
    document.addEventListener("visibilitychange", this.visibility)
    void this.cycle()
    return () => {
      this.stopped = true
      this.controller?.abort()
      this.abortRetries()
      clearTimeout(this.timer)
      document.removeEventListener("visibilitychange", this.visibility)
    }
  }

  /** 单面板重试：只重跑指定面板，不打断正在进行的轮询周期。 */
  retry(panelId: string): void {
    if (this.stopped) return
    const instance = this.instances().find((entry) => entry.key === panelId)
    if (!instance) return
    this.abortRetry(panelId)
    const controller = new AbortController()
    this.retries.set(panelId, controller)
    void this.collect(instance, this.domain(), controller.signal)
      .finally(() => { if (this.retries.get(panelId) === controller) this.retries.delete(panelId) })
  }

  /** 相对范围的 now 按看板 now delay 回退；绝对范围保持不变。 */
  private domain(): { from: string; to: string } {
    return dashboardDomain(this.options.range, this.options.definition)
  }

  private shiftedNow(): number {
    const delay = this.options.range.mode === "relative" ? Math.max(0, this.options.definition.now_delay_seconds ?? 0) : 0
    return Date.now() - delay * 1000
  }

  /** 重复面板按变量展开成实例；非重复面板保持与面板同键。 */
  private instances(): WorkspacePanelInstance[] {
    // 行与文本面板不出查询：它们只参与渲染，加入队列会白跑一轮空请求。
    return workspacePanelInstances(this.options.definition, this.options.selections, this.options.nodes, this.options.groups)
      .filter((instance) => !isStaticPanel(instance.base))
  }

  private abortRetry(panelId: string): void {
    this.retries.get(panelId)?.abort()
    this.retries.delete(panelId)
  }

  private abortRetries(): void {
    for (const controller of this.retries.values()) controller.abort()
    this.retries.clear()
  }

  private async cycle(): Promise<void> {
    if (this.stopped || document.hidden) return
    this.controller?.abort()
    this.abortRetries()
    clearTimeout(this.timer)
    const controller = new AbortController()
    this.controller = controller
    this.completed = false
    this.retryDelay = 0
    const domain = this.domain()
    const instances = this.instances()
    this.update({ type: "range", domain, dashboardId: this.options.dashboardId, panelIds: instances.map((instance) => instance.key) })
    await runWorkspaceQueue(instances, (instance) => this.collect(instance, domain, controller.signal), controller.signal)
    if (this.stopped || controller.signal.aborted || this.controller !== controller) return
    this.completed = true
    this.update({ type: "complete" })
    if (!document.hidden && this.interval > 0) this.timer = setTimeout(() => { void this.cycle() }, Math.max(this.interval, this.retryDelay))
  }

  private async collect(instance: WorkspacePanelInstance, domain: { from: string; to: string }, signal: AbortSignal): Promise<void> {
    const id = instance.key
    this.update({ type: "panel", id, patch: { loading: true, error: undefined } })
    try {
      const expanded = expandPanelSeries(instance.panel, { ...this.options, binding: instance.binding })
      // 面板级时间覆盖只影响这个面板的查询，看板其余部分仍用统一的看板范围。
      const query = panelMetricQuery(expanded, panelQueryDomain(instance.panel, domain, this.shiftedNow()), panelQueryPoints(instance.panel, this.options.definition))
      this.update({ type: "panel", id, patch: { query, expanded } })
      const result = await queryWorkspaceMetrics(query, { signal, public: this.options.public })
      if (!signal.aborted) this.update({ type: "panel", id, patch: { result, resultQuery: query, resultExpanded: expanded, updatedAt: Date.now(), loading: false } })
    } catch (error) {
      this.retryDelay = Math.max(this.retryDelay, retryAfterDelay(error))
      if (!signal.aborted) this.update({ type: "panel", id, patch: { loading: false, error: error instanceof Error ? error.message : "workspace.query_failed" } })
    }
  }
}
