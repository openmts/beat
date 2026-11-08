import { useMemo, type ReactNode } from "react"
import { PublicThemeContext, publicThemeStyles, themeShell, themeSummary } from "@/lib/theme-presentation"
import type { ThemeDefinition } from "@/types/theme-definition"
import type { ThemeSnapshot } from "@/types/theme-runtime"

interface ProviderProps {
  definition: ThemeDefinition
  snapshot: ThemeSnapshot
  children: ReactNode
  onNodeSelect?: (id: string) => void
  surface?: "page" | "embedded"
}
export function PublicThemeProvider({ definition, snapshot, children, onNodeSelect, surface = "page" }: ProviderProps) {
  const style = useMemo(() => publicThemeStyles(definition, snapshot.theme), [definition, snapshot.theme])
  const value = useMemo(() => ({ definition, snapshot, onNodeSelect }), [definition, snapshot, onNodeSelect])
  const shell = themeShell(definition)
  const summary = themeSummary(definition)
  return <PublicThemeContext.Provider value={value}>
    <div data-public-theme={definition.overview} data-theme-detail={definition.detail} data-theme-density={definition.density}
      data-theme-surface={definition.surface ?? "solid"} data-theme-summary={summary.layout}
      data-theme-shell-background={shell.background} data-theme-shell-header={shell.header} data-theme-shell-footer={shell.footer}
      className={`${surface === "page" ? "min-h-svh bg-background" : ""} text-foreground ${snapshot.theme === "dark" ? "dark" : ""}`} style={style}>{children}</div>
  </PublicThemeContext.Provider>
}
