import { Route, Routes } from "react-router"
import AdminLayout from "@/pages/admin/layout"
import Groups from "@/pages/admin/groups"
import Nodes from "@/pages/admin/nodes"
import SSHKeys from "@/pages/admin/ssh-keys"
import Terminal from "@/pages/admin/terminal"
import Alerts from "@/pages/admin/alerts"
import Network from "@/pages/admin/network"
import Settings from "@/pages/admin/settings"
import AdminLogin from "@/pages/admin/login"
import Security from "@/pages/admin/security"
import { AuthProvider, useAuth } from "@/context/auth"

function Admin() {
  return (
    <AuthProvider>
      <AdminAccess />
    </AuthProvider>
  )
}

function AdminAccess() {
	const { authenticated, loading } = useAuth()
	if (loading) return <main className="grid min-h-screen place-items-center text-sm text-muted-foreground">Loading...</main>
  if (!authenticated) return <AdminLogin />
  return (
    <Routes>
      <Route element={<AdminLayout />}>
        <Route index element={<Groups />} />
        <Route path="groups" element={<Groups />} />
        <Route path="nodes" element={<Nodes />} />
        <Route path="ssh-keys" element={<SSHKeys />} />
        <Route path="terminal" element={<Terminal />} />
        <Route path="alerts" element={<Alerts />} />
        <Route path="network" element={<Network />} />
        <Route path="settings" element={<Settings />} />
        <Route path="security" element={<Security />} />
      </Route>
    </Routes>
  )
}

export default Admin
