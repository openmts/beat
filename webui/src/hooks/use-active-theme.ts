import { useEffect, useState } from "react"
import { getActiveTheme } from "@/lib/theme-api"
import type { ActiveThemeDescriptor } from "@/types/theme"

export function useActiveTheme() {
  const [descriptor, setDescriptor] = useState<ActiveThemeDescriptor | null>(null)
  useEffect(() => {
    let cancelled = false
    getActiveTheme().then((value) => { if (!cancelled) setDescriptor(value) })
      .catch(() => { if (!cancelled) setDescriptor(null) })
    return () => { cancelled = true }
  }, [])
  return descriptor
}
