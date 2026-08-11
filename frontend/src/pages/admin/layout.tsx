import { Outlet } from "react-router"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import AdminSidebar from "@/components/admin-sidebar"
import AdminHeader from "@/components/admin-header"

function AdminLayout() {
  return (
    <SidebarProvider>
      <AdminSidebar />
      <SidebarInset>
        <AdminHeader />
        <main className="flex-1 p-4 safe-pb sm:p-6">
          <div className="container-page max-w-[90rem]">
            <Outlet />
          </div>
        </main>
      </SidebarInset>
    </SidebarProvider>
  )
}

export default AdminLayout
