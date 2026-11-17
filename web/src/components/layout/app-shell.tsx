import { useEffect, useRef, useState } from "react";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ChevronRight,
  Command,
  ExternalLink,
  Gauge,
  Layers3,
  ListChecks,
  LoaderCircle,
  LogOut,
  Menu,
  Moon,
  Network,
  Palette,
  Radio,
  Search,
  Server,
  Settings2,
  Sun,
  X,
  type LucideIcon,
} from "lucide-react";
import { api } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const navigation: Array<{ href: string; label: string; icon: LucideIcon; match: (path: string) => boolean }> = [
  { href: "/admin", label: "运行总览", icon: Gauge, match: (path) => path === "/admin" || path === "/admin/" },
  { href: "/admin/nodes", label: "节点管理", icon: Server, match: (path) => path.startsWith("/admin/nodes") },
  { href: "/admin/groups", label: "分组管理", icon: Layers3, match: (path) => path.startsWith("/admin/groups") },
  { href: "/admin/tasks", label: "任务中心", icon: ListChecks, match: (path) => path.startsWith("/admin/tasks") },
  { href: "/admin/agents", label: "节点接入", icon: Radio, match: (path) => path.startsWith("/admin/agents") },
  { href: "/admin/settings", label: "系统设置", icon: Settings2, match: (path) => path.startsWith("/admin/settings") },
  { href: "/admin/theme", label: "前台主题", icon: Palette, match: (path) => path.startsWith("/admin/theme") },
];

const titles: Record<string, string> = {
  "/admin": "运行总览",
  "/admin/": "运行总览",
  "/admin/nodes": "节点管理",
  "/admin/groups": "分组管理",
  "/admin/tasks": "任务中心",
  "/admin/agents": "节点接入",
  "/admin/settings": "系统设置",
  "/admin/theme": "前台主题",
};

export function AppShell() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const { theme, toggleTheme } = useTheme();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [query, setQuery] = useState("");
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const sidebarRef = useRef<HTMLElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const auth = useQuery({ queryKey: ["auth", "me"], queryFn: api.authMe, refetchInterval: 60_000 });
  const meta = useQuery({ queryKey: ["meta"], queryFn: api.meta, refetchInterval: 30_000 });
  const authenticated = auth.data?.authenticated === true;
  const settings = useQuery({ queryKey: ["settings"], queryFn: api.settings, refetchInterval: 30_000, enabled: authenticated });
  const logout = useMutation({
    mutationFn: api.logout,
    onSettled: () => {
      queryClient.clear();
      void navigate({ to: "/login", replace: true });
    },
  });

  useEffect(() => {
    setMobileOpen(false);
    mainRef.current?.scrollTo(0, 0);
  }, [pathname]);

  useEffect(() => {
    if (!auth.isPending && !authenticated) {
      void navigate({ to: "/login", replace: true });
    }
  }, [auth.isPending, authenticated, navigate]);

  useEffect(() => {
    const invalidate = () => queryClient.setQueryData(["auth", "me"], { authenticated: false });
    window.addEventListener("beat:unauthorized", invalidate);
    return () => window.removeEventListener("beat:unauthorized", invalidate);
  }, [queryClient]);

  useEffect(() => {
    if (!mobileOpen) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setMobileOpen(false);
        menuButtonRef.current?.focus();
      }
    };
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("keydown", close);
    };
  }, [mobileOpen]);

  function submitSearch(event: React.FormEvent) {
    event.preventDefault();
    if (!query.trim()) return;
    void navigate({ to: "/admin/nodes", search: { q: query.trim() } });
  }

  const pageTitle = pathname.startsWith("/admin/nodes/") ? "节点详情" : titles[pathname] ?? "Beat";
  const mtsReady = settings.data?.storage.mts.ready ?? false;
  const siteName = meta.data?.site.name || "Beat";
  const siteSubtitle = meta.data?.site.subtitle || "Node telemetry";
  const runtimeLabel = meta.isError ? "服务连接异常" : meta.data?.demoMode ? "演示环境" : meta.data ? "服务运行中" : "服务连接中";
  const runtimeDotClass = meta.isError ? "bg-destructive" : meta.data ? "bg-success" : "bg-warning";
  const serverVersion = meta.data?.version ? `v${meta.data.version.replace(/^v/, "")}` : "—";

  useEffect(() => {
    document.title = `${pageTitle} · ${siteName}`;
  }, [pageTitle, siteName]);

  if (auth.isPending || !authenticated) {
    return <div className="grid min-h-screen place-items-center bg-background text-sm text-muted-foreground"><span className="flex items-center gap-2"><LoaderCircle className="h-4 w-4 animate-spin" />{auth.isPending ? "正在验证登录状态…" : "正在前往登录页…"}</span></div>;
  }

  return (
    <div className="flex h-dvh min-h-0 overflow-hidden bg-background">
      {mobileOpen ? <button type="button" aria-label="关闭导航" className="fixed inset-0 z-40 bg-slate-950/35 backdrop-blur-[1px] lg:hidden" onClick={() => setMobileOpen(false)} /> : null}
      <aside ref={sidebarRef} className={cn(
        "fixed inset-y-0 left-0 z-50 flex w-[248px] shrink-0 flex-col border-r bg-card transition-transform duration-200 lg:static lg:h-full lg:translate-x-0 lg:transition-none",
        mobileOpen ? "translate-x-0 shadow-float" : "-translate-x-full",
      )}>
        <div className="flex h-16 items-center justify-between border-b px-5">
          <Link to="/admin" className="flex items-center gap-2.5">
            <div className="relative grid h-9 w-9 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm">
              <Activity className="h-[19px] w-[19px]" strokeWidth={2.4} />
              <span className="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-card bg-success" />
            </div>
            <div>
              <p className="max-w-[140px] truncate text-[15px] font-semibold leading-none tracking-tight">{siteName}</p>
              <p className="mt-1 max-w-[140px] truncate text-[10px] uppercase tracking-[0.18em] text-muted-foreground">{siteSubtitle}</p>
            </div>
          </Link>
          <Button ref={menuButtonRef} variant="ghost" size="icon" className="lg:hidden" onClick={() => setMobileOpen(false)} aria-label="关闭导航"><X className="h-4 w-4" /></Button>
        </div>

        <div className="px-4 pt-5">
          <div className="flex items-center justify-between rounded-lg border bg-background/70 px-3 py-2.5">
            <div className="flex items-center gap-2 text-xs">
              <span className="relative flex h-2 w-2">{meta.data && !meta.isError ? <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-40" /> : null}<span className={cn("relative inline-flex h-2 w-2 rounded-full", runtimeDotClass)} /></span>
              <span className="font-medium">{runtimeLabel}</span>
            </div>
            <Badge variant="outline" className="font-mono text-[9px]" title={meta.data?.version ? `Beat Server ${meta.data.version}` : "Server 版本读取中"}>{serverVersion}</Badge>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 py-5 scrollbar-thin">
          <p className="px-3 pb-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70">监控</p>
          <div className="space-y-1">
            {navigation.slice(0, 4).map((item) => <NavItem key={item.href} item={item} pathname={pathname} />)}
          </div>
          <p className="px-3 pb-2 pt-6 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70">平台</p>
          <div className="space-y-1">
            {navigation.slice(4).map((item) => <NavItem key={item.href} item={item} pathname={pathname} />)}
          </div>
        </nav>

        <div className="border-t p-4">
          <Link to="/" className="mb-3 flex items-center justify-between rounded-lg border bg-background/60 px-3 py-2 text-[11px] font-medium text-muted-foreground transition-colors hover:border-primary/20 hover:text-primary">
            <span className="flex items-center gap-2"><ExternalLink className="h-3.5 w-3.5" />查看前台节点视图</span><ChevronRight className="h-3 w-3" />
          </Link>
          <div className="rounded-xl border bg-gradient-to-br from-slate-950 to-slate-800 p-3.5 text-white shadow-sm dark:from-slate-900 dark:to-slate-950">
            <div className="flex items-center gap-2 text-xs font-medium"><Network className="h-3.5 w-3.5 text-sky-300" />MTS 时序存储</div>
            <div className="mt-3 flex items-center justify-between text-[10px] text-slate-300">
              <span className="flex items-center gap-1.5"><span className={cn("h-1.5 w-1.5 rounded-full", mtsReady ? "bg-emerald-400" : "bg-amber-400 animate-pulse-soft")} />{settings.isError ? "连接异常" : mtsReady ? "运行正常" : "连接中"}</span>
              <span className="font-mono">embedded</span>
            </div>
            <div className="mt-2 h-1 overflow-hidden rounded-full bg-white/10"><div className={cn("h-full rounded-full", mtsReady ? "w-[92%] bg-emerald-400" : "w-1/3 bg-amber-400")} /></div>
          </div>
          <div className="mt-3 flex items-center justify-between px-1 text-[10px] text-muted-foreground">
            <span>Beat Server</span><span className="font-mono">{settings.data?.server.version ?? "0.1.0"}</span>
          </div>
        </div>
      </aside>

      <div className="flex h-full min-w-0 flex-1 flex-col">
        <header className="z-30 flex h-16 shrink-0 items-center gap-3 border-b bg-background/85 px-4 backdrop-blur-md sm:px-6">
          <Button ref={menuButtonRef} variant="ghost" size="icon" className="lg:hidden" onClick={() => setMobileOpen(true)} aria-label="打开导航"><Menu className="h-5 w-5" /></Button>
          <div className="hidden min-w-0 items-center gap-1.5 text-xs text-muted-foreground sm:flex">
            <span>Beat</span><ChevronRight className="h-3.5 w-3.5" /><span className="font-medium text-foreground">{pageTitle}</span>
          </div>
          <form onSubmit={submitSearch} className="relative ml-auto hidden w-full max-w-xs md:block">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索节点、IP 或标签…" className="h-9 w-full rounded-lg border bg-card pl-9 pr-12 text-xs shadow-sm outline-none transition focus:border-primary/40 focus:ring-2 focus:ring-primary/10" />
            <span className="pointer-events-none absolute right-2.5 top-1/2 flex -translate-y-1/2 items-center gap-0.5 rounded border bg-muted/60 px-1.5 py-0.5 font-mono text-[9px] text-muted-foreground"><Command className="h-2.5 w-2.5" />K</span>
          </form>
          <div className="ml-auto flex items-center gap-1.5 md:ml-0">
            <Badge variant="success" className="hidden sm:inline-flex"><span className="h-1.5 w-1.5 animate-pulse rounded-full bg-success" />实时采集</Badge>
            <Link to="/" className="hidden h-8 items-center gap-1.5 rounded-md border bg-card px-2.5 text-[11px] font-medium text-muted-foreground shadow-sm transition-colors hover:text-foreground sm:flex"><ExternalLink className="h-3 w-3" />前台</Link>
            <Button variant="ghost" size="icon" onClick={toggleTheme} aria-label="切换主题">{theme === "light" ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}</Button>
            <div className="ml-1 flex items-center gap-1.5 rounded-full border bg-card p-0.5 pl-2 shadow-sm">
              <span className="hidden max-w-24 truncate text-[11px] font-medium text-muted-foreground sm:block">{auth.data?.username}</span>
              <span className="grid h-7 w-7 place-items-center rounded-full bg-gradient-to-br from-primary to-sky-400 text-[11px] font-semibold uppercase text-white">{(auth.data?.username || siteName).slice(0, 1)}</span>
              <Button variant="ghost" size="icon" className="h-7 w-7 rounded-full" onClick={() => logout.mutate()} disabled={logout.isPending} aria-label="退出登录" title="退出登录">
                {logout.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <LogOut className="h-3.5 w-3.5" />}
              </Button>
            </div>
          </div>
        </header>
        <main ref={mainRef} className={cn("min-h-0 flex-1 overscroll-contain scrollbar-thin", mobileOpen ? "overflow-hidden" : "overflow-y-auto")}>
          <div className="mx-auto w-full min-w-0 max-w-[1440px] px-4 py-6 sm:px-6 lg:py-7">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}

function NavItem({ item, pathname }: { item: (typeof navigation)[number]; pathname: string }) {
  const active = item.match(pathname);
  const Icon = item.icon;
  return (
    <Link to={item.href} className={cn(
      "group flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13px] font-medium transition-colors",
      active ? "bg-primary/10 text-primary" : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
    )}>
      <Icon className={cn("h-4 w-4", active ? "text-primary" : "text-muted-foreground/75 group-hover:text-foreground")} strokeWidth={active ? 2.2 : 1.8} />
      <span>{item.label}</span>
    </Link>
  );
}

