import { useEffect } from "react";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Activity, ArrowUpRight, LayoutDashboard, Moon, Sun } from "lucide-react";
import { api } from "@/api/client";
import { DEFAULT_PUBLIC_THEME, PublicThemeRoot, publicContentWidthClass, usePublicTheme } from "@/features/public/theme/public-theme";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export function PublicShell() {
  const bootstrap = useQuery({ queryKey: ["public-bootstrap"], queryFn: api.publicBootstrap, refetchInterval: 30_000 });
  const themeDocument = bootstrap.data?.theme ?? DEFAULT_PUBLIC_THEME;
  return (
    <PublicThemeRoot document={themeDocument}>
      <PublicShellContent />
    </PublicThemeRoot>
  );
}

function PublicShellContent() {
  const { resolvedMode, toggleMode, document: themeDocument } = usePublicTheme();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const detailView = pathname.startsWith("/nodes/");
  const bootstrap = useQuery({ queryKey: ["public-bootstrap"], queryFn: api.publicBootstrap, refetchInterval: 30_000 });
  const siteName = bootstrap.data?.site.name || "Beat";
  const siteSubtitle = bootstrap.data?.site.subtitle || "Node observatory";
  const width = publicContentWidthClass(themeDocument.appearance.contentWidth);
  const minimal = themeDocument.presetId === "minimal";

  useEffect(() => {
    document.title = `${detailView ? "指标详情" : "节点广场"} · ${siteName}`;
  }, [detailView, siteName]);

  return (
    <div className="min-h-screen">
      <header className={cn("sticky top-0 z-30 border-b backdrop-blur-xl", minimal ? "bg-background/95" : "bg-background/80")}>
        <div className={cn("mx-auto flex w-full items-center gap-4 px-4 sm:px-6 lg:px-8", minimal ? "h-14" : "h-16", width)}>
          <Link to="/" className="flex items-center gap-2.5">
            <div className={cn("relative grid place-items-center bg-primary text-primary-foreground", minimal ? "h-8 w-8 rounded-md shadow-none" : "h-9 w-9 rounded-xl shadow-sm shadow-primary/20")}>
              <Activity className="h-[19px] w-[19px]" strokeWidth={2.4} />
              {!minimal ? <span className="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-card bg-success" /> : null}
            </div>
            <div>
              <p className="max-w-[150px] truncate text-[15px] font-semibold leading-none tracking-tight">{siteName}</p>
              {!minimal ? <p className="mt-1 max-w-[150px] truncate text-[9px] uppercase tracking-[0.2em] text-muted-foreground">{siteSubtitle}</p> : null}
            </div>
          </Link>

          <nav className="ml-5 hidden items-center gap-1 sm:flex">
            <Link to="/" className={cn("rounded-md px-3 py-2 text-xs font-medium transition-colors", detailView ? "text-muted-foreground hover:bg-muted hover:text-foreground" : minimal ? "text-foreground" : "bg-primary/[0.08] text-primary")}>节点广场</Link>
            {detailView ? <span className={cn("rounded-md px-3 py-2 text-xs font-medium", minimal ? "text-muted-foreground" : "bg-primary/[0.08] text-primary")}>指标详情</span> : null}
          </nav>

          <div className="ml-auto flex items-center gap-1.5">
            <Badge variant="success" className={cn("hidden md:inline-flex", minimal && "md:hidden")}><span className="h-1.5 w-1.5 animate-pulse rounded-full bg-success" />实时采集</Badge>
            <button type="button" onClick={toggleMode} aria-label="切换主题" className="grid h-8 w-8 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
              {resolvedMode === "light" ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
            </button>
            <Link to="/admin" className={buttonVariants({ variant: minimal ? "ghost" : "outline", size: "sm" })}>
              <LayoutDashboard className="h-3.5 w-3.5" />管理后台<ArrowUpRight className="hidden h-3 w-3 sm:block" />
            </Link>
          </div>
        </div>
      </header>

      <main className={cn("mx-auto w-full px-4 sm:px-6 lg:px-8", minimal ? "py-4 sm:py-5" : "py-7 sm:py-9 lg:py-10", width)}>
        <Outlet />
      </main>

      <footer className={cn("mx-auto w-full flex-col gap-2 px-4 pb-8 text-[10px] text-muted-foreground sm:flex-row sm:items-center sm:justify-between sm:px-6 lg:px-8", width, minimal ? "hidden" : "flex")}>
        <span>{siteName} · 面向网络节点的多值指标采集</span>
        <span>Agent gRPC 双向流 · MTS 时序存储 · SQLite 元数据</span>
      </footer>
    </div>
  );
}
