import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { useNavigate } from "react-router"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { useLocale } from "@/context/locale"
import { ThemeBridge } from "@/lib/theme-bridge"
import { themeAssetURL } from "@/lib/theme-protocol"
import type { ActiveThemeDescriptor } from "@/types/theme"
import { THEME_PROTOCOL, type ThemeFailure, type ThemeSnapshot } from "@/types/theme-runtime"

interface SandboxedThemeFrameProps {
  descriptor: ActiveThemeDescriptor
  snapshot: () => ThemeSnapshot
  fallback: ReactNode
  onFallback?: (reason: ThemeFailure) => void
  onNavigate?: (path: string) => void
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

function ThemeFrameSession({ descriptor, snapshot, fallback, onFallback, onNavigate, source }: SandboxedThemeFrameProps & { source: string }) {
  const routeNavigate = useNavigate()
  const navigate = onNavigate ?? routeNavigate
  const frameRef = useRef<HTMLIFrameElement>(null)
  const callbacks = useRef({ snapshot, onFallback, navigate })
  const [state, setState] = useState<{ ready: boolean; failure?: ThemeFailure }>({ ready: false })
  const [bridge] = useState(() => new ThemeBridge({
    window: () => frameRef.current?.contentWindow,
    snapshot: () => callbacks.current.snapshot(),
    navigate: (path) => { void callbacks.current.navigate(path) },
    ready: (ready) => setState({ ready }),
    fail: (failure) => { setState({ ready: false, failure }); callbacks.current.onFallback?.(failure) },
  }))
  useLayoutEffect(() => {
    callbacks.current = { snapshot, onFallback, navigate }
    bridge.publish()
  }, [bridge, snapshot, onFallback, navigate])
  useEffect(() => { bridge.start(); return () => bridge.stop() }, [bridge])
  if (state.failure) return <ThemeFallback reason={state.failure}>{fallback}</ThemeFallback>
  return (
    <div className="relative min-h-svh w-full">
      {!state.ready && fallback}
      <iframe ref={frameRef} title={descriptor.name} onLoad={() => bridge.loaded()} src={source}
        className={`h-svh min-h-svh w-full border-0 ${state.ready ? "block" : "invisible absolute inset-0"}`}
        aria-hidden={!state.ready} sandbox="allow-scripts" referrerPolicy="no-referrer" />
    </div>
  )
}

function ThemeFallback({ reason, children }: { reason: ThemeFailure; children: ReactNode }) {
  const { t } = useLocale()
  return <div data-theme-fallback={reason}>
    <Alert className="mx-auto my-3 max-w-5xl" role="status"><AlertDescription>{t(reason === "snapshot_budget" ? "theme.fallback_budget" : "theme.fallback_unavailable")}</AlertDescription></Alert>
    {children}
  </div>
}
