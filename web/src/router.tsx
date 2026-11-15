import { Link, Outlet, createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import { LoaderCircle } from "lucide-react";
import { AppShell } from "@/components/layout/app-shell";
import { PublicShell } from "@/components/layout/public-shell";
import { AgentsPage } from "@/features/agents/agents-page";
import { LoginPage } from "@/features/auth/login-page";
import { GroupsPage } from "@/features/groups/groups-page";
import { NodeDetailPage } from "@/features/nodes/node-detail-page";
import { NodesPage } from "@/features/nodes/nodes-page";
import { OverviewPage } from "@/features/overview/overview-page";
import { AdminThemePage } from "@/features/public/pages/admin-theme-page";
import { PublicHomePage } from "@/features/public/pages/public-home-page";
import { PublicNodeDetailPage } from "@/features/public/pages/public-node-detail-page";
import { SettingsPage } from "@/features/settings/settings-page";
import { TasksPage } from "@/features/tasks/tasks-page";

function RootComponent() {
  return <Outlet />;
}

function PendingComponent() {
  return <div className="grid min-h-64 place-items-center"><div className="flex items-center gap-2 text-sm text-muted-foreground"><LoaderCircle className="h-4 w-4 animate-spin" />正在加载页面…</div></div>;
}

const rootRoute = createRootRoute({
  component: RootComponent,
  notFoundComponent: () => (
    <div className="grid min-h-screen place-items-center bg-background p-6">
      <div className="w-full max-w-md rounded-xl border bg-card p-8 text-center shadow-panel">
        <p className="text-5xl font-semibold tracking-tight text-primary">404</p>
        <h1 className="mt-4 text-lg font-semibold">页面不存在</h1>
        <p className="mt-2 text-sm text-muted-foreground">请求的页面可能已移动，或地址输入有误。</p>
        <Link to="/" className="mt-6 inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">返回节点视图</Link>
      </div>
    </div>
  ),
});

const publicRoute = createRoute({ getParentRoute: () => rootRoute, id: "public", component: PublicShell });
const foregroundHomeRoute = createRoute({ getParentRoute: () => publicRoute, path: "/", component: PublicHomePage });
const publicNodeDetailRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/nodes/$nodeId",
  component: function PublicNodeDetailRoute() {
    const { nodeId } = publicNodeDetailRoute.useParams();
    return <PublicNodeDetailPage nodeId={nodeId} />;
  },
});

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });

const adminRoute = createRoute({ getParentRoute: () => rootRoute, path: "/admin", component: AppShell });
const overviewRoute = createRoute({ getParentRoute: () => adminRoute, path: "/", component: OverviewPage });
const nodesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/nodes",
  validateSearch: (search: Record<string, unknown>): { q?: string } => {
    const q = typeof search.q === "string" ? search.q.trim() : "";
    return q ? { q } : {};
  },
  component: NodesPage,
});
const nodeDetailRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/nodes/$nodeId",
  component: function NodeDetailRoute() {
    const { nodeId } = nodeDetailRoute.useParams();
    return <NodeDetailPage nodeId={nodeId} admin />;
  },
});
const agentsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/agents", component: AgentsPage });
const groupsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/groups", component: GroupsPage });
const tasksRoute = createRoute({ getParentRoute: () => adminRoute, path: "/tasks", component: TasksPage });
const settingsRoute = createRoute({ getParentRoute: () => adminRoute, path: "/settings", component: SettingsPage });
const themeRoute = createRoute({ getParentRoute: () => adminRoute, path: "/theme", component: AdminThemePage });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  publicRoute.addChildren([foregroundHomeRoute, publicNodeDetailRoute]),
  adminRoute.addChildren([overviewRoute, nodesRoute, nodeDetailRoute, groupsRoute, tasksRoute, agentsRoute, settingsRoute, themeRoute]),
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  defaultPendingComponent: PendingComponent,
  defaultPendingMs: 120,
  defaultPendingMinMs: 220,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

