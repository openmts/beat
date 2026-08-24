import { act, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { SandboxedThemeFrame, validateFrameMessage } from "@/components/sandboxed-theme-frame"
import type { ActiveThemeDescriptor } from "@/types/theme"

const frameWindow = {}
const snapshot = {
  beat: { version: 1, type: "ready" },
}
const payloadMessage = {
  beat: { version: 1, type: "navigate", payload: "#/nodes" },
}

describe("sandbox postMessage validation", () => {
  it("accepts a valid ready message from the frame", () => {
    const result = validateFrameMessage(frameWindow, frameWindow, snapshot)
    expect(result).toEqual({ type: "ready", payload: undefined })
  })

  it("accepts an allowlisted navigate payload", () => {
    const result = validateFrameMessage(frameWindow, frameWindow, payloadMessage)
    expect(result?.type).toBe("navigate")
    expect(result?.payload).toBe("#/nodes")
  })

  it("rejects messages from a different source", () => {
    expect(validateFrameMessage({}, frameWindow, snapshot)).toBeNull()
  })

  it("rejects messages with the wrong protocol version", () => {
    expect(validateFrameMessage(frameWindow, frameWindow, {
      beat: { version: 2, type: "ready" },
    })).toBeNull()
  })

  it("rejects disallowed message types", () => {
    expect(validateFrameMessage(frameWindow, frameWindow, {
      beat: { version: 1, type: "eval" },
    })).toBeNull()
  })

  it("rejects malformed or non-object data", () => {
    expect(validateFrameMessage(frameWindow, frameWindow, "not-an-object")).toBeNull()
    expect(validateFrameMessage(frameWindow, frameWindow, null)).toBeNull()
    expect(validateFrameMessage(frameWindow, frameWindow, {})).toBeNull()
  })

  it("rejects oversized payloads", () => {
    expect(validateFrameMessage(frameWindow, frameWindow, {
      beat: { version: 1, type: "ready", payload: "x".repeat(300_000) },
    })).toBeNull()
  })
})

describe("SandboxedThemeFrame component", () => {
  const descriptor: ActiveThemeDescriptor = {
    package_id: "p1", theme_id: "t1", name: "Sandbox", version: "1.0.0",
    runtime: "sandboxed", built_in: false, protocol: 1,
    sandbox_entry: "index.html", archive_digest: "a".repeat(64),
  }
  const fallback = <div>built-in-dashboard</div>
  const snapshot = () => ({ timestamp: "2026-01-01T00:00:00Z", nodes: [] })

  beforeEach(() => { vi.useFakeTimers() })

  it("renders the built-in fallback for non-sandbox themes", () => {
    render(
      <SandboxedThemeFrame descriptor={{ ...descriptor, runtime: "declarative", sandbox_entry: undefined, archive_digest: undefined }} snapshot={snapshot} fallback={fallback} />,
    )
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
  })

  it("renders the sandbox iframe for a sandboxed theme", () => {
    const onFallback = vi.fn()
    render(<SandboxedThemeFrame descriptor={descriptor} snapshot={snapshot} fallback={fallback} onFallback={onFallback} />)
    const frame = screen.getByTitle("Sandbox") as HTMLIFrameElement
    expect(frame).toBeInTheDocument()
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(frame.src).toContain(`/themes/assets/${descriptor.archive_digest}/index.html`)
    vi.advanceTimersByTime(6000)
    expect(onFallback).toHaveBeenCalled()
  })

  it("clears the timeout after receiving a ready message", () => {
    const onFallback = vi.fn()
    render(<SandboxedThemeFrame descriptor={descriptor} snapshot={snapshot} fallback={fallback} onFallback={onFallback} />)
    const frame = screen.getByTitle("Sandbox") as HTMLIFrameElement
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        source: frame.contentWindow, data: { beat: { version: 1, type: "ready" } },
      }))
    })
    act(() => { vi.advanceTimersByTime(6000) })
    expect(onFallback).not.toHaveBeenCalled()
  })
})
