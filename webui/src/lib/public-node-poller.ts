import { APIError } from "@/lib/api-error"
import { retryAfterDelay } from "@/lib/retry-after"
import { getPublicNodeSummary } from "@/lib/public-workspace-api"
import type { Node } from "@/types"

export type PublicNodeEvent = { type: "loading" } | { type: "ready"; node: Node } | { type: "error"; unavailable: boolean }
interface NodePollingOptions { nodeId: string; refreshSeconds: number; update: (event: PublicNodeEvent) => void }

export class PublicNodePoller {
  private readonly options: NodePollingOptions
  private stopped = false
  private controller?: AbortController
  private timer?: ReturnType<typeof setTimeout>
  private loaded = false
  private readonly visibility = () => {
    if (document.hidden) { this.controller?.abort(); clearTimeout(this.timer) }
    else if (!this.loaded || this.options.refreshSeconds > 0) void this.cycle()
  }
  constructor(options: NodePollingOptions) { this.options = options }
  start(): () => void {
    document.addEventListener("visibilitychange", this.visibility)
    void this.cycle()
    return () => {
      this.stopped = true
      this.controller?.abort()
      clearTimeout(this.timer)
      document.removeEventListener("visibilitychange", this.visibility)
    }
  }
  private async cycle() {
    if (this.stopped || document.hidden) return
    this.controller?.abort()
    clearTimeout(this.timer)
    const controller = new AbortController()
    this.controller = controller
    this.options.update({ type: "loading" })
    let retryDelay = 0
    try {
      const node = await getPublicNodeSummary({ nodeId: this.options.nodeId, signal: controller.signal })
      if (!controller.signal.aborted) { this.loaded = true; this.options.update({ type: "ready", node }) }
    } catch (error) {
      retryDelay = retryAfterDelay(error)
      if (!controller.signal.aborted) this.options.update({ type: "error", unavailable: error instanceof APIError && [401, 403, 404].includes(error.status) })
    } finally {
      if (!this.stopped && this.controller === controller && !document.hidden && this.options.refreshSeconds > 0) {
        const interval = Math.min(300, Math.max(5, this.options.refreshSeconds)) * 1000
        this.timer = setTimeout(() => { void this.cycle() }, Math.max(interval, retryDelay))
      }
    }
  }
}
