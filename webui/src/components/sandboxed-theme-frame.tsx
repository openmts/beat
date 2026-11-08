import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { useNavigate } from "react-router"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { useLocale } from "@/context/locale"
import { ThemeBridge } from "@/lib/theme-bridge"
import { themeAssetURL } from "@/lib/theme-protocol"
import type { ActiveThemeDescriptor } from "@/types/theme"
import { THEME_PROTOCOL, type ThemeFailure, type ThemeSnapshot } from "@/types/theme-runtime"
import type { WorkspaceRange } from "@/types/metric-workspace"

interface SandboxedThemeFrameProps {
  descriptor: ActiveThemeDescriptor
  snapshot: () => ThemeSnapshot
  fallback: ReactNode
  onFallback?: (reason: ThemeFailure) => void
  onNavigate?: (path: string) => void
  onSetRange?: (range: WorkspaceRange) => void
}

export function SandboxedThemeFrame(props: SandboxedThemeFrameProps) {
  const descriptor = props.descriptor
  if (descriptor.runtime !== "sandboxed") return <>{props.fallback}</>
  let source: string
  try {
    if (descriptor.protocol !== THEME_PROTOCOL || !descriptor.sandbox_entry || !descriptor.archive_digest) throw new Error("invalid theme")
    source = themeAssetURL(descriptor.archive_digest, descriptor.sandbox_entry)
  } catch { return <ThemeFallback reason="protocol_error">{props.fallback}</ThemeFallback> }
  return <ThemeFrameSession key={source} {...props} source={source} />
}

function ThemeFrameSession({ descriptor, snapshot, fallback, onFallback, onNavigate, onSetRange, source }: SandboxedThemeFrameProps & { source: string }) {
  const routeNavigate = useNavigate()
  const navigate = onNavigate ?? routeNavigate
  const frameRef = useRef<HTMLIFrameElement>(null)
  const callbacks = useRef({ snapshot, onFallback, navigate, onSetRange })
  const [state, setState] = useState<{ ready: boolean; failure?: ThemeFailure }>({ ready: false })
  const [bridge] = useState(() => new ThemeBridge({
    window: () => frameRef.current?.contentWindow,
    snapshot: () => callbacks.current.snapshot(),
    navigate: (path) => { void callbacks.current.navigate(path) },
    setRange: (range) => callbacks.current.onSetRange?.(range),
    ready: (ready) => setState({ ready }),
    fail: (failure) => { setState({ ready: false, failure }); callbacks.current.onFallback?.(failure) },
  }))
  useLayoutEffect(() => {
    callbacks.current = { snapshot, onFallback, navigate, onSetRange }
    bridge.publish()
  }, [bridge, snapshot, onFallback, navigate, onSetRange])
  useEffect(() => { bridge.start(); return () => bridge.stop() }, [bridge])
  if (state.failure) return <ThemeFallback reason={state.failure}>{fallback}</ThemeFallback>
  return <div className="relative min-h-svh w-full bg-background">
    <iframe ref={frameRef} title={descriptor.name} onLoad={() => bridge.loaded()} src={source}
      className="block h-svh min-h-svh w-full border-0" aria-hidden={!state.ready}
      sandbox="allow-scripts" referrerPolicy="no-referrer" />
  </div>
}

function ThemeFallback({ reason, children }: { reason: ThemeFailure; children: ReactNode }) {
  const { t } = useLocale()
  return <div data-theme-fallback={reason}>
    <Alert className="mx-auto my-3 max-w-5xl" role="status"><AlertDescription>{t(reason === "snapshot_budget" ? "theme.fallback_budget" : "theme.fallback_unavailable")}</AlertDescription></Alert>
    {children}
  </div>
}
