import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Eye, Home, Layers3, LoaderCircle, MonitorSmartphone, Palette, ShieldCheck } from "lucide-react";
import { api } from "@/api/client";
import type { PublicThemeDocument, PublicThemePreset } from "@/api/types";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Segmented } from "@/components/ui/segmented";
import { PublicHomePage } from "@/features/public/pages/public-home-page";
import { PublicNodeDetailPage } from "@/features/public/pages/public-node-detail-page";
import { PublicThemeRoot } from "@/features/public/theme/public-theme";
import { cn } from "@/lib/utils";

type PreviewPage = "home" | "nodeDetail";

export function AdminThemePage() {
  const queryClient = useQueryClient();
  const catalog = useQuery({ queryKey: ["admin-theme-catalog"], queryFn: api.themeCatalog });
  const published = useQuery({ queryKey: ["public-theme"], queryFn: api.publicTheme });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [previewPage, setPreviewPage] = useState<PreviewPage>("home");
  const [mobilePane, setMobilePane] = useState<"library" | "preview">("library");
  const [notice, setNotice] = useState<string | null>(null);

  const presets = catalog.data?.items ?? [];
  const selected = presets.find((preset) => preset.id === selectedId) ?? presets[0];
  const appliedPreset = published.data ? presets.find((preset) => themesMatch(published.data.document, preset.document)) : undefined;
  const applied = selected && published.data ? themesMatch(published.data.document, selected.document) : false;

  useEffect(() => {
    if (selectedId || !published.data || presets.length === 0) return;
    const matched = presets.find((preset) => themesMatch(published.data.document, preset.document));
    setSelectedId(matched?.id ?? presets[0].id);
  }, [presets, published.data, selectedId]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 7_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const applyTheme = useMutation({
    mutationFn: async (document: PublicThemeDocument) => {
      await api.saveThemeDraft(document);
      return api.publishTheme();
    },
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ["public-theme"] });
      void queryClient.invalidateQueries({ queryKey: ["public-bootstrap"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-theme-catalog"] });
      setNotice(`主题已应用，线上版本更新为 revision ${result.revision}。`);
    },
  });

  if (catalog.isPending || published.isPending) return <LoadingState label="正在读取主题库…" />;
  if (catalog.isError) return <ErrorState message={catalog.error.message} onRetry={() => void catalog.refetch()} />;
  if (published.isError) return <ErrorState message={published.error.message} onRetry={() => void published.refetch()} />;
  if (!selected) return <ErrorState message="主题库为空，请联系管理员检查 Beat 服务端主题配置。" onRetry={() => void catalog.refetch()} />;

  const busy = applyTheme.isPending;
  const appliedName = appliedPreset?.name ?? "自定义主题版本";

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Public experience"
        title="前台主题"
        description="从 Beat 内置主题中选择一套即可。主题包含前台外观、首页模块与节点详情布局，无需逐项配置。"
        actions={<>
          <Badge variant="outline" className="h-8 gap-1.5 px-3"><Palette className="h-3 w-3" />当前：{appliedName}</Badge>
          <Button type="button" size="sm" onClick={() => applyTheme.mutate(selected.document)} disabled={applied || busy}>
            {applyTheme.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
            {applied ? "当前已使用" : "应用此主题"}
          </Button>
        </>}
      />

      <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-card px-4 py-3 text-xs shadow-panel">
        <Badge variant="success" className="gap-1.5"><span className="h-1.5 w-1.5 rounded-full bg-success" />前台运行正常</Badge>
        <span className="text-muted-foreground">当前线上版本 <span className="font-mono text-foreground">r{published.data.revision}</span></span>
        <span className="text-muted-foreground">已应用 <span className="font-medium text-foreground">{appliedName}</span></span>
        <span className="hidden items-center gap-1 text-muted-foreground sm:flex"><ShieldCheck className="h-3.5 w-3.5" />主题由 Beat 统一维护，数据权限不受主题配置影响</span>
        <Badge variant="outline" className="ml-auto gap-1"><Layers3 className="h-3 w-3" />{presets.length} 套内置主题</Badge>
      </div>

      {notice ? <div className="flex items-start gap-2 rounded-xl border border-success/20 bg-success/[0.06] px-4 py-3 text-xs text-success"><CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />{notice}</div> : null}
      {applyTheme.isError ? <div className="rounded-xl border border-destructive/20 bg-destructive/[0.05] px-4 py-3 text-xs text-destructive">{applyTheme.error.message}</div> : null}

      <div className="flex xl:hidden"><Segmented value={mobilePane} onChange={setMobilePane} options={[{ value: "library", label: "主题库" }, { value: "preview", label: "实时预览" }]} /></div>

      <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(330px,400px)_minmax(0,1fr)]">
        <div className={cn("min-w-0 space-y-4", mobilePane === "preview" && "hidden xl:block")}>
          <Card>
            <CardHeader className="border-b pb-3">
              <div className="flex items-center justify-between gap-3"><CardTitle className="flex items-center gap-2"><Palette className="h-4 w-4 text-primary" />主题库</CardTitle><Badge variant="outline">{presets.length} 套</Badge></div>
              <p className="text-[11px] text-muted-foreground">点击主题即可在上方预览，确认后应用到线上前台。</p>
            </CardHeader>
            <CardContent className="space-y-3 pt-4">
              {presets.map((preset) => (
                <ThemePresetCard
                  key={preset.id}
                  preset={preset}
                  selected={preset.id === selected.id}
                  applied={Boolean(appliedPreset && appliedPreset.id === preset.id)}
                  onSelect={() => setSelectedId(preset.id)}
                />
              ))}
              <div className="rounded-xl border border-dashed bg-muted/[0.12] p-3 text-[11px] leading-5 text-muted-foreground">
                主题由 Beat 统一维护。选择任意主题即可预览，确认后应用到线上前台。
              </div>
            </CardContent>
          </Card>

          <div className="rounded-xl border bg-card p-4 shadow-panel xl:hidden">
            <Button type="button" className="w-full" onClick={() => applyTheme.mutate(selected.document)} disabled={applied || busy}>
              {applyTheme.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
              {applied ? "当前已使用此主题" : `应用「${selected.name}」`}
            </Button>
          </div>
        </div>

        <div className={cn("min-w-0", mobilePane === "library" && "hidden xl:block")}>
          <ThemePreview preset={selected} page={previewPage} setPage={setPreviewPage} revision={published.data.revision} />
        </div>
      </div>
    </div>
  );
}

function ThemePresetCard({ preset, selected, applied, onSelect }: { preset: PublicThemePreset; selected: boolean; applied: boolean; onSelect: () => void }) {
  const primary = preset.document.appearance.primary;
  const accent = preset.document.appearance.accent;
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      className={cn("group block w-full overflow-hidden rounded-xl border text-left transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", selected ? "border-primary/35 shadow-float ring-1 ring-primary/10" : "hover:border-primary/20 hover:shadow-panel")}
    >
      <div className="relative h-28 overflow-hidden border-b p-4" style={{ background: `radial-gradient(circle at 18% 0%, ${primary}24, transparent 45%), radial-gradient(circle at 100% 10%, ${accent}1f, transparent 38%), linear-gradient(145deg, ${primary}0d, transparent 65%)` }}>
        <div className="flex items-center justify-between"><span className="rounded-md border border-white/30 bg-white/70 px-2 py-1 text-[9px] font-semibold text-slate-700 shadow-sm backdrop-blur dark:bg-slate-900/65 dark:text-white">{preset.id === "minimal" ? "极简白" : preset.document.appearance.surface === "gradient" ? "渐变观测" : "内置主题"}</span><span className="h-8 w-8 rounded-xl shadow-sm" style={{ background: `linear-gradient(135deg, ${primary}, ${accent})` }} /></div>
        <div className="mt-5 grid grid-cols-3 gap-1.5"><span className="h-1.5 rounded-full bg-slate-900/10 dark:bg-white/20" /><span className="h-1.5 rounded-full bg-slate-900/10 dark:bg-white/20" /><span className="h-1.5 rounded-full bg-slate-900/10 dark:bg-white/20" /></div>
        <div className="mt-2 h-8 rounded-lg border border-white/35 bg-white/45 backdrop-blur-sm dark:border-white/10 dark:bg-slate-900/25" />
      </div>
      <div className="bg-card p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0"><p className="truncate text-sm font-semibold">{preset.name}</p><p className="mt-1 line-clamp-2 text-[11px] leading-5 text-muted-foreground">{preset.description}</p></div>
          {applied ? <Badge variant="success" className="shrink-0 px-1.5 py-0 text-[8px]">使用中</Badge> : preset.isDefault ? <Badge variant="outline" className="shrink-0 px-1.5 py-0 text-[8px]">默认</Badge> : null}
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[9px] text-muted-foreground">
          <span className="rounded border bg-background/70 px-1.5 py-0.5">首页 {preset.document.home.blocks.filter((block) => block.enabled).length} 模块</span>
          <span className="rounded border bg-background/70 px-1.5 py-0.5">详情 {preset.document.nodeDetail.blocks.filter((block) => block.enabled).length} 模块</span>
          <span className="ml-auto flex items-center gap-1"><span className="h-2 w-2 rounded-full" style={{ background: primary }} /><span className="h-2 w-2 rounded-full" style={{ background: accent }} /></span>
        </div>
      </div>
    </button>
  );
}

function ThemePreview({ preset, page, setPage, revision }: { preset: PublicThemePreset; page: PreviewPage; setPage: (page: PreviewPage) => void; revision: number }) {
  const nodesQuery = useQuery({ queryKey: ["public-nodes", "", "all"], queryFn: () => api.publicNodes(), enabled: page === "nodeDetail", refetchInterval: 30_000 });
  const nodeId = nodesQuery.data?.items[0]?.id;
  return (
    <div className="space-y-3 xl:sticky xl:top-20">
      <div className="flex flex-col gap-3 rounded-xl border bg-card p-3 shadow-panel sm:flex-row sm:items-center sm:justify-between">
        <div><p className="flex items-center gap-2 text-xs font-semibold"><Eye className="h-3.5 w-3.5 text-primary" />主题实时预览</p><p className="mt-0.5 text-[10px] text-muted-foreground">使用公开 API 的真实数据预览「{preset.name}」· 线上 revision {revision}</p></div>
        <Segmented value={page} onChange={setPage} options={[{ value: "home", label: "首页" }, { value: "nodeDetail", label: "节点详情" }]} />
      </div>
      <div className="public-theme-preview overflow-hidden rounded-xl border bg-background shadow-panel">
        <div className="flex items-center gap-2 border-b bg-muted/35 px-3 py-2 text-[10px] text-muted-foreground"><span className="flex gap-1"><span className="h-2 w-2 rounded-full bg-destructive/65" /><span className="h-2 w-2 rounded-full bg-warning/65" /><span className="h-2 w-2 rounded-full bg-success/65" /></span><span className="ml-2 flex items-center gap-1 font-mono">{page === "home" ? <Home className="h-3 w-3" /> : <MonitorSmartphone className="h-3 w-3" />}beat / {page === "home" ? "nodes" : "nodes/:id"}</span><Badge variant="outline" className="ml-auto px-1.5 py-0 text-[8px]">PREVIEW</Badge></div>
        <div className="h-[calc(100dvh-15rem)] min-h-[520px] overflow-y-auto overscroll-contain">
          <PublicThemeRoot document={preset.document} className="min-h-full">
            <div className="mx-auto max-w-[1360px] px-4 py-6">
              {page === "home" ? <PublicHomePage /> : nodeId ? <PublicNodeDetailPage nodeId={nodeId} /> : nodesQuery.isPending ? <LoadingState label="正在选择预览节点…" /> : <div className="rounded-xl border border-dashed p-8 text-center text-xs text-muted-foreground">暂无节点可用于详情预览</div>}
            </div>
          </PublicThemeRoot>
        </div>
      </div>
    </div>
  );
}

function themesMatch(left: PublicThemeDocument, right: PublicThemeDocument) {
  return themeSignature(left) === themeSignature(right);
}

function themeSignature(document: PublicThemeDocument) {
  return JSON.stringify({
    appearance: document.appearance,
    home: normalizePageForSignature(document.home),
    nodeDetail: normalizePageForSignature(document.nodeDetail),
  });
}

function normalizePageForSignature(page: PublicThemeDocument["home"]) {
  return { blocks: page.blocks.map((block) => ({ ...block, props: block.props ?? {} })) };
}
