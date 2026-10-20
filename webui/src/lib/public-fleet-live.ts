import { mergeAcceleratorSamples } from "@/lib/accelerator-public"
import type { Group, Node, MetricsSnapshot } from "@/types"
import { retryAfterDelay } from "@/lib/retry-after"
import { FleetReadError, maximumFleetNodes, readPublicFleet } from "@/lib/public-fleet-api"
export interface PublicFleetSnapshot { nodes: Node[]; groups: Group[]; loading: boolean; error: string; connected: boolean; offline: boolean; receivedAt: number | null; serverTime: number | null; stale: boolean }
interface Options { publish: (state: PublicFleetSnapshot) => void; read?: typeof readPublicFleet; socket?: (url: string) => WebSocket }
const reconcileInterval = 30000, staleAfter = 60000, maximumFrameBytes = 8 * 1024 * 1024
export class PublicFleetLive {
 private state: PublicFleetSnapshot = { nodes: [], groups: [], loading: true, error: "", connected: false, offline: false, receivedAt: null, serverTime: null, stale: false }
 private stopped = false; private request?: AbortController; private socket?: WebSocket
 private poll?: ReturnType<typeof setTimeout>; private retry?: ReturnType<typeof setTimeout>; private age?: ReturnType<typeof setInterval>
 private publishedStale = false
 private sequence = 0; private streamVersion = 0; private streamTime = 0; private attempts = 0; private opened = false
 private readonly options: Options
 constructor(options: Options) { this.options = options }
 start() {
  window.addEventListener("online", this.resume); window.addEventListener("offline", this.suspend); document.addEventListener("visibilitychange", this.visibility)
  this.age = setInterval(() => { if (!document.hidden && navigator.onLine && this.stale() !== this.publishedStale) this.emit() }, 1000); this.resume()
  return () => { this.stopped = true; this.pause(); clearInterval(this.age); window.removeEventListener("online", this.resume); window.removeEventListener("offline", this.suspend); document.removeEventListener("visibilitychange", this.visibility) }
 }
 refresh = async () => {
  if (this.stopped || document.hidden || !navigator.onLine) return
  this.request?.abort(); clearTimeout(this.poll)
  const controller = new AbortController(), sequence = ++this.sequence, streamVersion = this.streamVersion
  this.request = controller
  let retryDelay = 0
  try {
   const response = await (this.options.read ?? readPublicFleet)(controller.signal)
   if (controller.signal.aborted || sequence !== this.sequence || this.stopped) return
   const latest = new Map(this.state.nodes.map(node => [node.id, node]))
   const nodes = response.nodes.map(node => streamVersion !== this.streamVersion && latest.has(node.id) ? mergeLiveNode(node, latest.get(node.id)!) : node)
   this.state = { ...this.state, nodes, groups: response.groups, loading: false, error: "", offline: false, receivedAt: Date.now(), serverTime: response.serverTime ?? this.state.serverTime ?? Date.now() }
   this.emit(); this.connect()
  } catch (error) {
   if (controller.signal.aborted || this.stopped) return
   retryDelay = retryAfterDelay(error)
   const clear = error instanceof FleetReadError && ["denied", "capacity"].includes(error.kind)
   this.state = { ...this.state, ...(clear ? { nodes: [], groups: [] } : {}), loading: false, error: error instanceof FleetReadError ? error.kind : "unavailable" }; this.emit()
  } finally { if (!this.stopped && !controller.signal.aborted && !document.hidden && navigator.onLine) this.poll = setTimeout(() => { void this.refresh() }, Math.max(reconcileInterval, retryDelay)) }
 }
 private stale() { return Boolean(this.state.error || this.state.receivedAt !== null && Date.now() - this.state.receivedAt > staleAfter) }
 private emit() { this.publishedStale = this.stale(); this.options.publish({ ...this.state, stale: this.publishedStale }) }
 private visibility = () => { if (document.hidden) this.pause(); else this.resume() }
 private suspend = () => { this.pause(); this.state = { ...this.state, nodes: [], groups: [], loading: false, offline: true, connected: false }; this.emit() }
 private resume = () => { if (this.stopped) return; if (!navigator.onLine) { this.suspend(); return }; if (!document.hidden) { this.state.offline = false; this.emit(); void this.refresh() } }
 private pause() {
  this.sequence++; this.request?.abort(); clearTimeout(this.poll); clearTimeout(this.retry)
  if (this.socket) { this.socket.onclose = null; this.socket.onmessage = null; this.socket.onopen = null; this.socket.onerror = null; this.socket.close(); this.socket = undefined }
  this.state.connected = false
 }
 private connect() {
  if (this.socket || this.stopped || document.hidden || !navigator.onLine) return
  const protocol = location.protocol === "https:" ? "wss:" : "ws:"
  let socket: WebSocket
  try { socket = (this.options.socket ?? (url => new WebSocket(url)))(`${protocol}//${location.host}/api/v1/ws/metrics`) } catch { this.reconnect(); return }
  this.socket = socket
  socket.onopen = () => { if (this.socket !== socket || this.stopped) return; this.attempts = 0; this.state.connected = true; this.emit(); if (this.opened) void this.refresh(); this.opened = true }
  socket.onmessage = event => { if (this.socket === socket && !this.stopped) this.message(event.data) }
  socket.onerror = () => { this.diagnose("error", socket); socket.close() }
  socket.onclose = event => { if (this.socket !== socket) return; this.diagnose("close", socket, event); this.socket = undefined; this.state.connected = false; this.emit(); this.reconnect() }
 }
 // diagnose 把浏览器拒绝暴露的连接结果写进控制台：onerror 本身不带状态码，
 // 只有 close 事件的 code/reason 与重试次数能区分被拦截、被拒绝与正常断开。
 private diagnose(phase: "error" | "close", socket: WebSocket, event?: CloseEvent) {
  if (phase === "close" && event?.code === 1000 && event.wasClean) return
  console.warn(`[metrics-ws] ${phase}`, { url: socket.url, readyState: socket.readyState, code: event?.code, reason: event?.reason, wasClean: event?.wasClean, attempts: this.attempts, hidden: document.hidden, online: navigator.onLine })
 }
 private reconnect() {
  clearTimeout(this.retry)
  if (this.stopped || document.hidden || !navigator.onLine) return
  const delay = Math.min(30000, 1000 * 2 ** Math.min(this.attempts++, 5)) * (0.75 + Math.random() * 0.5)
  this.retry = setTimeout(() => this.connect(), delay)
 }
 private message(raw: unknown) {
  if (typeof raw !== "string" || raw.length > maximumFrameBytes || new TextEncoder().encode(raw).byteLength > maximumFrameBytes) return
  try {
   const frame = JSON.parse(raw) as MetricsSnapshot, at = Date.parse(frame.timestamp)
   if (!Number.isFinite(at) || at <= this.streamTime || !Array.isArray(frame.nodes) || frame.nodes.length > maximumFleetNodes) return
   if (new Set(frame.nodes.map(node => node.id)).size !== frame.nodes.length) return
   const incoming = new Map(frame.nodes.filter(node => node.is_public === true).map(node => [node.id, node]))
   if (!this.state.nodes.some(node => incoming.has(node.id))) return
   this.streamTime = at
   this.streamVersion++
   this.state = { ...this.state, nodes: this.state.nodes.map(node => incoming.has(node.id) ? mergeLiveNode(node, incoming.get(node.id)!) : node), receivedAt: Date.now(), serverTime: at }
   this.emit()
  } catch { return }
 }
}
export function mergeLiveNode(metadata: Node, current: Node): Node {
 return { ...metadata, accelerators: mergeAcceleratorSamples(metadata.accelerators, current.accelerators), status: ["online", "offline"].includes(current.status) ? current.status : metadata.status, metrics: current.metrics ?? metadata.metrics, telemetry_at: current.telemetry_at, telemetry_age_seconds: current.telemetry_age_seconds, telemetry_state: current.telemetry_state ?? "unavailable", traffic: current.traffic ?? metadata.traffic, last_seen: current.last_seen ?? metadata.last_seen }
}
