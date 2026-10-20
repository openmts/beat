export const loadDashboard = () => import("@/pages/dashboard")
export const loadNodeDetail = () => import("@/pages/node-detail")
export const loadAdmin = () => import("@/pages/admin")
export const loadNotFound = () => import("@/pages/not-found")
export function preloadNodeDetail() { void loadNodeDetail().catch(() => undefined) }
export function preloadAdmin() { void loadAdmin().catch(() => undefined) }
