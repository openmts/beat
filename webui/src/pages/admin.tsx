import "@/context/admin-catalogs"
import { useEffect, useState, type ReactNode } from "react"
import { LazyRoute } from "@/components/route-chunk"
import { Route, Routes } from "react-router"
import AdminLayout from "@/pages/admin/layout"
import AdminLogin from "@/pages/admin/login"
import { AuthProvider, useAuth } from "@/context/auth"
import { adminLocalePending, loadAdminLocale } from "@/context/admin-locales"
import { initialLocale } from "@/context/locales"
import { useLocale } from "@/context/locale"

const loadGroups = () => import("@/pages/admin/groups")
const loadNodes = () => import("@/pages/admin/nodes")
const loadAgentSetup = () => import("@/pages/admin/agent-setup")
const loadEnrollment = () => import("@/pages/admin/enrollment")
const loadSSHKeys = () => import("@/pages/admin/ssh-keys")
const loadOperations = () => import("@/pages/admin/operations")
const loadTerminal = () => import("@/pages/admin/terminal")
const loadCustomMetrics = () => import("@/pages/admin/custom-metrics")
const loadMetricsWorkspace = () => import("@/pages/admin/metrics")
const loadMetricsExplore = () => import("@/pages/admin/metrics-explore")
const loadAlerts = () => import("@/pages/admin/alerts")
const loadNetwork = () => import("@/pages/admin/network")
const loadSettings = () => import("@/pages/admin/settings")
const loadAgentReleases = () => import("@/pages/admin/agent-releases")
const loadAccelerators = () => import("@/pages/admin/accelerators")
const loadLifecycle = () => import("@/pages/admin/lifecycle")
const loadNotFound = () => import("@/pages/not-found")
const loadSecurity = () => import("@/pages/admin/security")

// 分片请求在模块求值时立即发起（早于 React 挂载），管理端首屏因此不必等两跳。
void loadAdminLocale(initialLocale(localStorage, navigator))

// 管理端文案按语言分片懒加载：分片就绪前只显示与鉴权阶段一致的占位，
// 保证首屏与语言切换都不会先渲染英文再跳转目标语言。
function AdminLocaleGate({ children }: { children: ReactNode }) {
  const { locale } = useLocale()
  const [ready, setReady] = useState(() => !adminLocalePending(locale))
  useEffect(() => {
    if (!adminLocalePending(locale)) {
      setReady(true)
      return
    }
    let active = true
    setReady(false)
    void loadAdminLocale(locale).finally(() => { if (active) setReady(true) })
    return () => { active = false }
  }, [locale])
  if (!ready) {
    return <main className="grid min-h-screen place-items-center text-sm text-muted-foreground">Loading...</main>
  }
  return children
}

function Admin() {
  return (
    <AdminLocaleGate>
      <AuthProvider>
        <AdminAccess />
      </AuthProvider>
    </AdminLocaleGate>
  )
}

function AdminAccess() {
	const { authenticated, loading } = useAuth()
	if (loading) return <main className="grid min-h-screen place-items-center text-sm text-muted-foreground">Loading...</main>
  if (!authenticated) return <AdminLogin />
  return (
    <Routes>
      <Route element={<AdminLayout />}>
        <Route index element={<LazyRoute load={loadGroups} />} />
        <Route path="groups" element={<LazyRoute load={loadGroups} />} />
        <Route path="nodes" element={<LazyRoute load={loadNodes} />} />
        <Route path="agent-setup" element={<LazyRoute load={loadAgentSetup} />} />
        <Route path="enrollment" element={<LazyRoute load={loadEnrollment} />} />
        <Route path="ssh-keys" element={<LazyRoute load={loadSSHKeys} />} />
        <Route path="terminal" element={<LazyRoute load={loadTerminal} />} />
        <Route path="operations" element={<LazyRoute load={loadOperations} />} />
        <Route path="custom-metrics" element={<LazyRoute load={loadCustomMetrics} />} />
        <Route path="metrics" element={<LazyRoute load={loadMetricsWorkspace} />} />
        <Route path="metrics/explore" element={<LazyRoute load={loadMetricsExplore} />} />
        <Route path="alerts" element={<LazyRoute load={loadAlerts} />} />
        <Route path="network" element={<LazyRoute load={loadNetwork} />} />
        <Route path="settings" element={<LazyRoute load={loadSettings} />} />
        <Route path="agent-releases" element={<LazyRoute load={loadAgentReleases} />} />
        <Route path="accelerators" element={<LazyRoute load={loadAccelerators} />} />
        <Route path="lifecycle" element={<LazyRoute load={loadLifecycle} />} />
        <Route path="security" element={<LazyRoute load={loadSecurity} />} />
        <Route path="*" element={<LazyRoute load={loadNotFound} />} />
      </Route>
    </Routes>
  )
}

export default Admin
