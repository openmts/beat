import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { subscribeAuthInvalidated } from "@/lib/auth"
import {
  bootstrapAdmin,
  getAdminSession,
  getAuthState,
  loginAdmin,
  logoutAdmin,
} from "@/lib/security-api"
import type { AdminPrincipal } from "@/types/security"

interface LoginInput {
  username: string
  password: string
  totpCode?: string
}

interface BootstrapInput extends LoginInput {
  bootstrapToken: string
  displayName: string
}

interface AuthContextValue {
  authenticated: boolean
  loading: boolean
  setupRequired: boolean
  principal: AdminPrincipal | null
  login: (input: LoginInput) => Promise<void>
  bootstrap: (input: BootstrapInput) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [setupRequired, setSetupRequired] = useState(false)
  const [principal, setPrincipal] = useState<AdminPrincipal | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const state = await getAuthState()
      setSetupRequired(state.setup_required)
      if (state.setup_required) {
        setPrincipal(null)
        return
      }
      try {
        setPrincipal(await getAdminSession())
      } catch {
        setPrincipal(null)
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    return subscribeAuthInvalidated(() => setPrincipal(null))
  }, [refresh])

  const login = useCallback(async (input: LoginInput) => {
    const next = await loginAdmin({
      username: input.username.trim(),
      password: input.password,
      totp_code: input.totpCode?.trim(),
    })
    setSetupRequired(false)
    setPrincipal(next)
  }, [])

  const bootstrap = useCallback(async (input: BootstrapInput) => {
    const next = await bootstrapAdmin({
      bootstrap_token: input.bootstrapToken.trim(),
      username: input.username.trim(),
      display_name: input.displayName.trim(),
      password: input.password,
    })
    setSetupRequired(false)
    setPrincipal(next)
  }, [])

  const logout = useCallback(async () => {
    try {
      await logoutAdmin()
    } finally {
      setPrincipal(null)
    }
  }, [])

  const value = useMemo<AuthContextValue>(() => ({
    authenticated: principal !== null,
    loading,
    setupRequired,
    principal,
    login,
    bootstrap,
    logout,
    refresh,
  }), [bootstrap, loading, login, logout, principal, refresh, setupRequired])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) throw new Error("useAuth must be used within AuthProvider")
  return context
}
