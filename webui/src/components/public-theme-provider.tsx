import { useMemo, type ReactNode } from "react"
import { PublicThemeContext, publicThemeStyles } from "@/lib/theme-presentation"
import type { ThemeDefinition } from "@/types/theme-definition"
import type { ThemeSnapshot } from "@/types/theme-runtime"

interface ProviderProps {
  definition: ThemeDefinition
  snapshot: ThemeSnapshot
  children: ReactNode
  onNodeSelect?: (id: string) => void
}
export function PublicThemeProvider({ definition, snapshot, children, onNodeSelect }: ProviderProps) {
  const style = useMemo(() => publicThemeStyles(definition, snapshot.theme), [definition, snapshot.theme])
  const value = useMemo(() => ({ definition, snapshot, onNodeSelect }), [definition, snapshot, onNodeSelect])
  return <PublicThemeContext.Provider value={value}>
    <div data-public-theme={definition.overview} data-theme-detail={definition.detail} data-theme-density={definition.density}
      className={`min-h-svh bg-background text-foreground ${snapshot.theme === "dark" ? "dark" : ""}`} style={style}>{children}</div>
  </PublicThemeContext.Provider>
}
