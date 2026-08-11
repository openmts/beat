import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react"
import type { SiteTheme } from "@/types"

type Theme = "light" | "dark"

interface ThemeContextValue {
  theme: Theme
  toggleTheme: () => void
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

function getStoredTheme(defaultTheme: SiteTheme): SiteTheme {
	const stored = localStorage.getItem("theme")
	if (stored === "light" || stored === "dark") return stored
	return defaultTheme
}

export function ThemeProvider({
  children,
  defaultTheme = "system",
}: {
  children: ReactNode
  defaultTheme?: SiteTheme
}) {
  const [media] = useState(() => window.matchMedia("(prefers-color-scheme: dark)"))
  const [systemTheme, setSystemTheme] = useState<Theme>(media.matches ? "dark" : "light")
  const [preference, setPreference] = useState<SiteTheme>(() => getStoredTheme(defaultTheme))
  const theme: Theme = preference === "system" ? systemTheme : preference

  useEffect(() => {
    if (localStorage.getItem("theme") === null) setPreference(defaultTheme)
  }, [defaultTheme])

  useEffect(() => {
    const listener = (event: MediaQueryListEvent) => {
      setSystemTheme(event.matches ? "dark" : "light")
    }
    media.addEventListener?.("change", listener)
    return () => media.removeEventListener?.("change", listener)
  }, [media])

  useEffect(() => {
    const root = document.documentElement
    root.classList.remove("light", "dark")
    root.classList.add(theme)
	}, [theme])

  const toggleTheme = useCallback(() => {
    const next = theme === "light" ? "dark" : "light"
    localStorage.setItem("theme", next)
    setPreference(next)
  }, [theme])

  return (
    <ThemeContext.Provider value={{ theme, toggleTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider")
  return ctx
}
