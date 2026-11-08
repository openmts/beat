import { useEffect, useState } from "react"
import { getActiveTheme } from "@/lib/theme-api"
import type { ActiveThemeDescriptor } from "@/types/theme"

export interface ActiveThemeState {
  status: "loading" | "ready" | "failed"
  descriptor: ActiveThemeDescriptor | null
}

let cached: ActiveThemeState = { status: "loading", descriptor: null }
let pending: Promise<void> | null = null

export function cachedActiveTheme(): ActiveThemeState {
  return cached
}

export function resetActiveThemeCache(): void {
  cached = { status: "loading", descriptor: null }
  pending = null
}

export async function preloadActiveTheme(): Promise<void> {
  if (cached.status !== "loading") return
  if (!pending) {
    pending = getActiveTheme()
      .then((descriptor) => { cached = { status: "ready", descriptor } })
      .catch(() => { cached = { status: "failed", descriptor: null } })
      .finally(() => { pending = null })
  }
  await pending
}

export function useActiveThemeState(): ActiveThemeState {
  const [state, setState] = useState(cached)
  useEffect(() => {
    if (cached.status !== "loading") { setState(cached); return }
    let cancelled = false
    void preloadActiveTheme().then(() => { if (!cancelled) setState(cached) })
    return () => { cancelled = true }
  }, [])
  return state
}

export function useActiveTheme() {
  return useActiveThemeState().descriptor
}
