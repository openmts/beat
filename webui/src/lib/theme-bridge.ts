import { MAX_THEME_SNAPSHOT_BYTES, THEME_PROTOCOL, type ThemeFailure, type ThemeSnapshot } from "@/types/theme-runtime"
import { boundedJSON, plainRecord, ThemeDataError } from "@/lib/theme-json"
import { frameMessage, newThemeInstance } from "@/lib/theme-protocol"

interface BridgeOptions {
  window: () => Window | null | undefined
  snapshot: () => ThemeSnapshot
  navigate: (path: string) => void
  ready: (value: boolean) => void
  fail: (reason: ThemeFailure) => void
}
const READY_TIMEOUT_MS = 5000
const HEARTBEAT_MS = 5000
const MAX_DOCUMENT_LOADS = 3

export class ThemeBridge {
  private instance = ""
  private ready = false
  private failed = false
  private loads = 0
  private timeout: number | undefined
  private heartbeat: number | undefined
  private visible = new Set<string>()
  private listener = (event: MessageEvent) => this.receive(event)

  private options: BridgeOptions
  constructor(options: BridgeOptions) { this.options = options }

  start(): void {
    this.armTimeout()
    this.heartbeat = window.setInterval(() => this.publish(), HEARTBEAT_MS)
    window.addEventListener("message", this.listener)
  }

  stop(): void {
    window.clearTimeout(this.timeout)
    window.clearInterval(this.heartbeat)
    window.removeEventListener("message", this.listener)
  }

  loaded(): void {
    if (this.failed) return
    if (++this.loads > MAX_DOCUMENT_LOADS) { this.fail("runtime_error"); return }
    if (this.instance) this.armTimeout()
    this.ready = false
    this.options.ready(false)
    try {
      this.instance = newThemeInstance()
      this.options.window()?.postMessage({ beat: { version: THEME_PROTOCOL, instance_id: this.instance, type: "init" } }, "*")
    } catch { this.fail("invalid_data") }
  }

  publish(): void {
    const target = this.options.window()
    if (!target || !this.ready || this.failed) return
    try {
      const payload = this.options.snapshot()
      const message = { beat: { version: THEME_PROTOCOL, instance_id: this.instance, type: "snapshot", payload } }
      boundedJSON(message, { depth: 14, keys: 100010, bytes: MAX_THEME_SNAPSHOT_BYTES })
      this.visible = new Set(payload.nodes.map((node) => node.id))
      target.postMessage(message, "*")
    } catch (error) { this.fail(error instanceof ThemeDataError ? error.code : "invalid_data") }
  }

  private receive(event: MessageEvent): void {
    const target = this.options.window()
    if (!target || event.source !== target || !this.instance || this.failed) return
    if (!plainRecord(event.data) || !plainRecord(event.data.beat)) { this.fail("protocol_error"); return }
    if (event.data.beat.instance_id !== this.instance) return
    const message = frameMessage(event, { window: target, instance: this.instance }, this.visible)
    if (!message) { this.fail("protocol_error"); return }
    if (message.type === "error") { this.fail("runtime_error"); return }
    if (message.type === "ready" && !this.ready) {
      window.clearTimeout(this.timeout)
      this.ready = true
      this.options.ready(true)
      this.publish()
    }
    if (message.type === "navigate" && this.ready) this.options.navigate(message.payload.path)
  }

  private armTimeout(): void {
    window.clearTimeout(this.timeout)
    this.timeout = window.setTimeout(() => this.fail("timeout"), READY_TIMEOUT_MS)
  }

  private fail(reason: ThemeFailure): void {
    if (this.failed) return
    this.failed = true
    this.ready = false
    this.stop()
    this.options.fail(reason)
  }
}
