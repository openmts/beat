import { useEffect, useRef, useState, type ReactNode } from "react"
import type { ActiveThemeDescriptor } from "@/types/theme"

// 沙箱 postMessage 协议版本与允许的消息类型。
const SANDBOX_PROTOCOL_VERSION = 1
const MAX_MESSAGE_BYTES = 256 * 1024
const READY_TIMEOUT_MS = 5000
const ALLOWED_TYPES = new Set(["ready", "navigate", "error"])

export interface SandboxSnapshot {
  timestamp: string
  nodes: unknown[]
}

// validateFrameMessage 校验来自沙箱的消息来源、协议版本、类型与大小。
export function validateFrameMessage(
  eventSource: unknown,
  frameWindow: unknown,
  data: unknown,
): { type: string; payload?: unknown } | null {
  if (eventSource !== frameWindow) return null
  if (typeof data !== "object" || data === null) return null
  const message = data as { beat?: { version?: number; type?: string; payload?: unknown } }
  if (!message.beat || message.beat.version !== SANDBOX_PROTOCOL_VERSION) return null
  const type = message.beat.type ?? ""
  if (!ALLOWED_TYPES.has(type)) return null
  const size = estimateBytes(data)
  if (size > MAX_MESSAGE_BYTES) return null
  return { type, payload: message.beat.payload }
}

function estimateBytes(value: unknown): number {
  try {
    return new Blob([JSON.stringify(value)]).size
  } catch {
    return MAX_MESSAGE_BYTES + 1
  }
}

interface SandboxedThemeFrameProps {
  descriptor: ActiveThemeDescriptor
  snapshot: () => SandboxSnapshot
  fallback: ReactNode
  onFallback?: () => void
}

export function SandboxedThemeFrame({
  descriptor,
  snapshot,
  fallback,
  onFallback,
}: SandboxedThemeFrameProps) {
  const frameRef = useRef<HTMLIFrameElement>(null)
  const [ready, setReady] = useState(false)
  const [timedOut, setTimedOut] = useState(false)

  const usable =
    descriptor.runtime === "sandboxed" &&
    descriptor.sandbox_entry &&
    descriptor.archive_digest

  useEffect(() => {
    if (!usable || ready) return
    const timer = window.setTimeout(() => {
      setTimedOut(true)
      onFallback?.()
    }, READY_TIMEOUT_MS)
    return () => window.clearTimeout(timer)
  }, [usable, ready, onFallback])

  useEffect(() => {
    if (!usable) return
    const handleMessage = (event: MessageEvent) => {
      const valid = validateFrameMessage(event.source, frameRef.current?.contentWindow, event.data)
      if (!valid) return
      if (valid.type === "ready") setReady(true)
      if (valid.type === "navigate" && typeof valid.payload === "string") {
        const target = valid.payload as string
        if (target.startsWith("#/") || target === "#") {
          window.location.hash = target === "#" ? "" : target.slice(1)
        }
      }
    }
    window.addEventListener("message", handleMessage)
    return () => window.removeEventListener("message", handleMessage)
  }, [usable])

  useEffect(() => {
    if (!usable || !ready || !frameRef.current?.contentWindow) return
    const sender = window.setInterval(() => {
      if (frameRef.current?.contentWindow) {
        frameRef.current.contentWindow.postMessage(
          { beat: { version: SANDBOX_PROTOCOL_VERSION, type: "snapshot", payload: snapshot() } },
          window.location.origin,
        )
      }
    }, 5000)
    return () => window.clearInterval(sender)
  }, [usable, ready, snapshot])

  if (!usable || timedOut) {
    return <>{fallback}</>
  }

  return (
    <div className="relative h-full w-full">
      {!ready && <div className="absolute inset-0">{fallback}</div>}
      <iframe
        ref={frameRef}
        title={descriptor.name}
        className="h-full w-full border-0"
        src={`/themes/assets/${descriptor.archive_digest}/${encodeURIComponent(descriptor.sandbox_entry ?? "")}`}
        sandbox="allow-scripts"
        referrerPolicy="no-referrer"
      />
    </div>
  )
}