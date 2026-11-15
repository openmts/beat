import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  Database,
  Globe2,
  HardDrive,
  Info,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  RotateCcw,
  Save,
  Server,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
  Timer,
  UserRound,
} from "lucide-react";
import type { SiteSettings, StorageSettings, SystemSettingsInput } from "@/api/types";
import { api } from "@/api/client";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { formatTime, formatUptime } from "@/lib/format";
import { cn } from "@/lib/utils";

const TIMEZONES = ["Asia/Shanghai", "Asia/Singapore", "Asia/Tokyo", "UTC", "Europe/London", "America/New_York"];

export function SettingsPage() {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["settings"], queryFn: api.settings, refetchInterval: 30_000 });
  const [draft, setDraft] = useState<SystemSettingsInput | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const saveSettings = useMutation({
    mutationFn: (input: SystemSettingsInput) => api.updateSettings(input),
    onSuccess: (result) => {
      setDraft(result.configuration);
      setNotice(result.restartRequired ? "设置已保存，MTS 存储参数将在重启服务后生效。" : "系统设置已保存并生效。");
      queryClient.setQueryData(["settings"], result);
      void queryClient.invalidateQueries({ queryKey: ["meta"] });
    },
  });

  useEffect(() => {
    if (settings.data && !draft) setDraft(settings.data.configuration);
  }, [settings.data, draft]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 8_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  if (settings.isPending || (!settings.isError && !draft)) return <LoadingState label="正在读取系统配置…" />;
  if (settings.isError) return <ErrorState message={settings.error.message} onRetry={() => void settings.refetch()} />;
  const data = settings.data;
  const configuration = draft ?? data.configuration;
  const dirty = JSON.stringify(configuration) !== JSON.stringify(data.configuration);
  const valid = configuration.site.name.trim().length > 0 && configuration.storage.mtsDataPath.trim().length > 0 && configuration.storage.retentionDays >= 1 && configuration.storage.retentionDays <= 365;
  const storageChanged = configuration.storage.mtsDataPath !== data.storage.mtsPath || configuration.storage.retentionDays !== data.storage.retentionDays || configuration.storage.walSync !== data.storage.walSync;
  const restartPending = data.restartRequired || (dirty && storageChanged);

  function updateSite<K extends keyof SiteSettings>(key: K, value: SiteSettings[K]) {
    setDraft((current) => current ? { ...current, site: { ...current.site, [key]: value } } : current);
  }

  function updateStorage<K extends keyof StorageSettings>(key: K, value: StorageSettings[K]) {
    setDraft((current) => current ? { ...current, storage: { ...current.storage, [key]: value } } : current);
  }

  function reset() {
    setDraft(data.configuration);
    saveSettings.reset();
    setNotice(null);
  }

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!dirty || !valid || saveSettings.isPending) return;
    saveSettings.mutate(configuration);
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Platform configuration"
        title="系统设置"
        description="集中管理站点身份、管理员账号与 MTS 时序存储；启动配置仅保留必要的监听地址和数据根目录。"
        actions={<>
          <Badge variant={data.storage.mts.ready ? "success" : "warning"} className="h-8 px-3"><span className={cn("h-1.5 w-1.5 rounded-full", data.storage.mts.ready ? "bg-success" : "bg-warning")} />{data.storage.mts.ready ? "MTS 运行正常" : "MTS 检查中"}</Badge>
          <Button type="button" variant="outline" size="sm" onClick={reset} disabled={!dirty || saveSettings.isPending}><RotateCcw className="h-3.5 w-3.5" />还原</Button>
          <Button type="submit" form="settings-form" size="sm" disabled={!dirty || !valid || saveSettings.isPending}>{saveSettings.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}保存设置</Button>
        </>}
      />

      {notice ? <div className="flex items-start gap-2 rounded-xl border border-success/20 bg-success/[0.06] px-4 py-3 text-xs text-success"><CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />{notice}</div> : null}
      {saveSettings.error ? <div className="flex items-start gap-2 rounded-xl border border-destructive/20 bg-destructive/[0.05] px-4 py-3 text-xs text-destructive"><AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{saveSettings.error.message}</div> : null}

      <AccountSecurityCard dataDir={data.storage.dataDir} />

      <form id="settings-form" onSubmit={submit}>
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_310px]">
          <div className="space-y-4">
            <Card id="site-settings" className="scroll-mt-24 overflow-hidden">
              <CardHeader className="border-b bg-gradient-to-r from-card to-primary/[0.025]"><div className="flex items-start justify-between gap-4"><div><CardTitle className="flex items-center gap-2"><Globe2 className="h-4 w-4 text-primary" />站点配置</CardTitle><p className="mt-1 text-xs text-muted-foreground">定义前台、后台导航中展示的站点身份与对外访问地址。</p></div><Badge variant="outline">立即生效</Badge></div></CardHeader>
              <CardContent className="space-y-5 pt-5">
                <div className="flex items-center gap-3 rounded-xl border bg-gradient-to-r from-primary/[0.07] via-sky-500/[0.04] to-transparent p-4">
                  <div className="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-primary text-lg font-semibold text-primary-foreground shadow-sm">{configuration.site.name.trim().slice(0, 1).toUpperCase() || "B"}</div>
                  <div className="min-w-0"><p className="truncate text-base font-semibold tracking-tight">{configuration.site.name || "未命名站点"}</p><p className="mt-1 truncate text-[10px] uppercase tracking-[0.18em] text-muted-foreground">{configuration.site.subtitle || "NODE OBSERVABILITY"}</p></div>
                  <Badge variant="success" className="ml-auto hidden sm:inline-flex">站点预览</Badge>
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="站点名称" hint="显示在前后台顶部，最多 64 个字符。"><Input value={configuration.site.name} onChange={(event) => updateSite("name", event.target.value)} maxLength={64} placeholder="Beat" /></Field>
                  <Field label="站点副标题" hint="用于补充环境或团队标识。"><Input value={configuration.site.subtitle} onChange={(event) => updateSite("subtitle", event.target.value)} maxLength={160} placeholder="NODE OBSERVABILITY" /></Field>
                  <Field label="默认时区" hint="用于后续报表与时间展示的默认时区。"><select value={configuration.site.timezone} onChange={(event) => updateSite("timezone", event.target.value)} className="flex h-9 w-full rounded-md border border-input bg-card px-3 text-sm shadow-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/30">{TIMEZONES.map((timezone) => <option key={timezone} value={timezone}>{timezone}</option>)}</select></Field>
                  <Field label="对外访问地址" hint="生成 Agent 接入命令时使用；留空自动跟随当前访问地址。"><Input value={configuration.site.publicUrl} onChange={(event) => updateSite("publicUrl", event.target.value)} placeholder="https://beat.example.com" type="url" /></Field>
                </div>
              </CardContent>
            </Card>

            <Card id="storage-settings" className="scroll-mt-24 overflow-hidden">
              <CardHeader className="border-b"><div className="flex items-start justify-between gap-4"><div><CardTitle className="flex items-center gap-2"><Database className="h-4 w-4 text-primary" />MTS 时序数据库</CardTitle><p className="mt-1 text-xs text-muted-foreground">管理嵌入式 MTS 的数据目录、保留周期与写入持久性。</p></div><Badge variant={restartPending ? "warning" : "success"}>{restartPending ? "等待重启" : "参数一致"}</Badge></div></CardHeader>
              <CardContent className="space-y-5 pt-5">
                <div className="grid gap-2 sm:grid-cols-3">
                  <MiniStat label="引擎状态" value={data.storage.mts.ready ? "Ready" : "Starting"} tone={data.storage.mts.ready ? "success" : "warning"} />
                  <MiniStat label="Measurement" value={String(data.storage.mts.measurements)} />
                  <MiniStat label="驱动" value={data.storage.mts.driver.toUpperCase()} />
                </div>

                <Field label="MTS 数据目录" hint="保存后需要重启 Server 才会切换；修改目录不会自动迁移已有历史指标，请确保目标目录可写。"><div className="relative"><HardDrive className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" /><Input value={configuration.storage.mtsDataPath} onChange={(event) => updateStorage("mtsDataPath", event.target.value)} className="pl-9 font-mono text-xs" placeholder="/var/lib/beat/mts" /></div></Field>

                <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_minmax(0,1fr)] sm:items-end">
                  <Field label="数据保留周期" hint="按天保留多值指标，范围 1–365 天。"><div className="relative"><Input type="number" min={1} max={365} value={configuration.storage.retentionDays} onChange={(event) => updateStorage("retentionDays", Number(event.target.value))} className="pr-12 tabular-nums" /><span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[11px] text-muted-foreground">天</span></div></Field>
                  <div className="rounded-lg border bg-background/55 p-3">
                    <div className="flex items-center justify-between gap-4"><div><p className="text-xs font-medium">WAL 同步落盘</p><p className="mt-1 text-[10px] leading-5 text-muted-foreground">开启后每次写入等待落盘，持久性更高、吞吐略低。</p></div><Toggle checked={configuration.storage.walSync} onChange={(checked) => updateStorage("walSync", checked)} label="WAL 同步落盘" /></div>
                  </div>
                </div>

                <div className={cn("flex items-start gap-2 rounded-lg border px-3 py-2.5 text-[10px] leading-5", restartPending ? "border-warning/25 bg-warning/[0.06] text-warning" : "border-border bg-muted/35 text-muted-foreground")}>
                  {restartPending ? <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
                  {restartPending ? "存储参数与当前运行实例不一致。保存后请重启 Beat Server，MTS 引擎将在启动时加载新配置。" : `当前运行目录：${data.storage.mtsPath} · 保留 ${data.storage.retentionDays} 天 · WAL ${data.storage.walSync ? "同步" : "异步"}`}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader><CardTitle className="flex items-center gap-2"><SlidersHorizontal className="h-4 w-4 text-primary" />运行参数与启动简化</CardTitle><p className="text-xs text-muted-foreground">启动配置通过 --config 加载 /etc/beat/server.yaml，命令行参数优先，同时兼容同名环境变量供容器使用。</p></CardHeader>
              <CardContent className="space-y-4">
                <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
                  <DefaultStat icon={Timer} label="采集间隔" value={`${data.collector.intervalSeconds}s`} />
                  <DefaultStat icon={Clock3} label="离线判定" value={`${data.collector.offlineAfterSeconds}s`} />
                  <DefaultStat icon={ShieldCheck} label="注册令牌" value={`${Math.round(data.collector.enrollmentTokenTTLSeconds / 60)} 分钟`} />
                  <DefaultStat icon={Database} label="默认查询" value={data.collector.defaultRange.toUpperCase()} />
                </div>
                <div className="rounded-lg border border-primary/15 bg-primary/[0.035] p-3">
                  <div className="flex flex-wrap items-center gap-2"><Settings2 className="h-3.5 w-3.5 text-primary" /><span className="text-[11px] font-medium">核心启动参数</span>{["--listen", "--data-dir", "--tls-cert", "--tls-key", "--demo"].map((key) => <code key={key} className="rounded border bg-card px-1.5 py-0.5 font-mono text-[9px] text-muted-foreground">{key}</code>)}</div>
                  <p className="mt-2 text-[10px] leading-5 text-muted-foreground">SQLite 路径由数据根目录自动推导；MTS 目录、保留周期和 WAL 策略由系统设置管理；Agent 二进制目录可通过 <code className="font-mono">--agent-binary-dir</code> 指定或自动发现。</p>
                </div>
              </CardContent>
            </Card>
          </div>

          <aside className="space-y-4 xl:sticky xl:top-20 xl:self-start">
            <Card className="overflow-hidden">
              <CardHeader className="border-b bg-muted/20"><div className="flex items-center justify-between gap-3"><CardTitle className="flex items-center gap-2"><Save className="h-4 w-4 text-primary" />保存状态</CardTitle><Badge variant={dirty ? "warning" : "success"}>{dirty ? "未保存" : "已同步"}</Badge></div></CardHeader>
              <CardContent className="space-y-4 pt-5">
                <div className="space-y-2 text-xs"><SideRow label="最近保存" value={data.updatedAt ? formatTime(data.updatedAt) : "使用默认配置"} /><SideRow label="站点标识" value={configuration.site.name || "—"} /><SideRow label="重启要求" value={restartPending ? "需要重启 Server" : "无需重启"} tone={restartPending ? "warning" : "success"} /></div>
                <Button type="submit" form="settings-form" className="w-full" disabled={!dirty || !valid || saveSettings.isPending}>{saveSettings.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}{saveSettings.isPending ? "正在保存…" : "保存设置"}</Button>
                <Button type="button" variant="outline" className="w-full" onClick={reset} disabled={!dirty || saveSettings.isPending}><RotateCcw className="h-4 w-4" />放弃修改</Button>
                {restartPending ? <div className="rounded-lg border border-warning/25 bg-warning/[0.06] p-3 text-[10px] leading-5 text-warning">保存成功后，执行 <code className="font-mono">systemctl restart beat-server</code> 应用 MTS 存储参数。</div> : <div className="rounded-lg border border-success/20 bg-success/[0.05] p-3 text-[10px] leading-5 text-success">当前编辑内容与运行实例一致，站点配置保存后立即更新。</div>}
              </CardContent>
            </Card>

            <Card>
              <CardHeader><CardTitle className="flex items-center gap-2"><Server className="h-4 w-4 text-primary" />运行环境</CardTitle><p className="text-xs text-muted-foreground">当前 Server 进程与关系库状态。</p></CardHeader>
              <CardContent className="space-y-2 text-xs">
                <SideRow label="版本" value={data.server.version} mono />
                <SideRow label="监听地址" value={data.server.listen} mono />
                <SideRow label="运行时长" value={formatUptime(data.server.uptimeSeconds)} />
                <SideRow label="演示模式" value={data.server.demoMode ? "开启" : "关闭"} />
                <div className="border-t pt-3"><p className="text-[10px] text-muted-foreground">SQLite 关系库</p><p className="mt-1 break-all font-mono text-[10px] leading-5">{data.storage.sqlitePath}</p></div>
              </CardContent>
            </Card>
          </aside>
        </div>
      </form>
    </div>
  );
}

function AccountSecurityCard({ dataDir }: { dataDir: string }) {
  const queryClient = useQueryClient();
  const auth = useQuery({ queryKey: ["auth", "me"], queryFn: api.authMe });
  const [username, setUsername] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  const updateCredentials = useMutation({
    mutationFn: api.updateCredentials,
    onSuccess: (result) => {
      setUsername(result.username);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setNotice("管理员凭据已更新，其他设备上的旧登录会话已失效。");
      queryClient.setQueryData(["auth", "me"], {
        authenticated: true,
        username: result.username,
        expiresAt: result.expiresAt,
      });
    },
  });

  useEffect(() => {
    if (auth.data?.username) setUsername(auth.data.username);
  }, [auth.data?.username]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 8_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const passwordChanged = newPassword.length > 0;
  const usernameChanged = username.trim() !== (auth.data?.username ?? "");
  const dirty = usernameChanged || passwordChanged;
  const passwordValid = !passwordChanged || (newPassword.length >= 8 && newPassword.length <= 128 && newPassword === confirmPassword);
  const valid = username.trim().length > 0 && username.trim().length <= 64 && currentPassword.length > 0 && passwordValid;
  const canSubmit = dirty && valid && !updateCredentials.isPending;
  const resetCommand = `sudo ./bin/beat-server reset-admin --data-dir ${dataDir.includes(" ") ? `'${dataDir.replace(/'/g, `'\\''`)}'` : dataDir}`;

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!canSubmit) return;
    updateCredentials.mutate({
      currentPassword,
      username: username.trim(),
      newPassword,
    });
  }

  return (
    <Card className="overflow-hidden">
      <CardHeader className="border-b bg-gradient-to-r from-card to-primary/[0.025]">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="flex items-center gap-2"><LockKeyhole className="h-4 w-4 text-primary" />账户与安全</CardTitle>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">Beat 仅支持一个管理员账号。修改账号或重置密码都需要验证当前密码，保存后其他设备上的登录会立即失效。</p>
          </div>
          <Badge variant="outline" className="w-fit shrink-0">单账号模式</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        <div className="flex flex-col gap-3 rounded-xl border bg-gradient-to-r from-primary/[0.06] via-sky-500/[0.035] to-transparent p-4 sm:flex-row sm:items-center">
          <div className="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm"><UserRound className="h-4 w-4" /></div>
          <div className="min-w-0">
            <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">当前登录账号</p>
            <p className="mt-1 truncate font-mono text-sm font-semibold">{auth.data?.username ?? "读取中…"}</p>
          </div>
          <div className="sm:ml-auto"><Badge variant="success"><ShieldCheck className="h-3 w-3" />已认证</Badge></div>
        </div>

        <form onSubmit={submit} className="space-y-4">
          <div className="grid gap-4 lg:grid-cols-2">
            <Field label="登录账号" hint="不允许包含空格或控制字符，最多 64 个字符。">
              <div className="relative">
                <UserRound className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" maxLength={64} className="pl-9" placeholder="admin" />
              </div>
            </Field>
            <Field label="当前密码" hint="为保护账户安全，更新任意凭据前必须验证当前密码。">
              <div className="relative">
                <KeyRound className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} autoComplete="current-password" className="pl-9" placeholder="输入当前登录密码" />
              </div>
            </Field>
            <Field label="新密码" hint="留空表示只修改登录账号；新密码为 8–128 个字符。">
              <Input type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} autoComplete="new-password" maxLength={128} placeholder="留空则不修改密码" />
            </Field>
            <Field label="确认新密码" hint="再次输入新密码，防止输入错误。">
              <Input type="password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} autoComplete="new-password" maxLength={128} placeholder="再次输入新密码" />
            </Field>
          </div>

          {passwordChanged && newPassword.length < 8 ? <p className="text-[10px] text-warning">新密码至少需要 8 个字符。</p> : null}
          {passwordChanged && confirmPassword && newPassword !== confirmPassword ? <p className="text-[10px] text-destructive">两次输入的新密码不一致。</p> : null}
          {updateCredentials.error ? <div className="flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2.5 text-xs text-destructive"><AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{updateCredentials.error.message}</div> : null}
          {notice ? <div className="flex items-start gap-2 rounded-lg border border-success/20 bg-success/[0.06] px-3 py-2.5 text-xs text-success"><CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />{notice}</div> : null}

          <div className="flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-[10px] leading-5 text-muted-foreground">保存后会轮换当前登录会话，但不会中断本次操作。</p>
            <Button type="submit" size="sm" disabled={!canSubmit}>
              {updateCredentials.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              {updateCredentials.isPending ? "正在更新…" : "更新账号与密码"}
            </Button>
          </div>
        </form>

        <div className="rounded-xl border border-warning/20 bg-warning/[0.045] p-3.5">
          <div className="flex items-center gap-2 text-xs font-medium text-warning"><Terminal className="h-3.5 w-3.5" />忘记密码时通过 Server 主机重置</div>
          <code className="mt-2.5 block overflow-x-auto whitespace-nowrap rounded-lg border bg-card px-3 py-2 font-mono text-[10px] text-foreground">{resetCommand}</code>
          <p className="mt-2 text-[10px] leading-5 text-muted-foreground">执行后账号与密码都会恢复为 <code className="rounded border bg-card px-1 py-0.5 font-mono">admin / admin</code>。服务运行期间执行也会立即删除持久会话；请根据实际数据目录调整 <code className="rounded border bg-card px-1 py-0.5 font-mono">--data-dir</code>。</p>
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ label, hint, children }: { label: string; hint: string; children: React.ReactNode }) {
  return <label className="block space-y-1.5"><span className="text-xs font-medium text-muted-foreground">{label}</span>{children}<span className="block text-[10px] font-normal leading-5 text-muted-foreground">{hint}</span></label>;
}

function MiniStat({ label, value, tone = "default" }: { label: string; value: string; tone?: "default" | "success" | "warning" }) {
  return <div className="rounded-lg bg-muted/40 p-3"><p className="text-[9px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{label}</p><p className={cn("mt-1 font-mono text-xs font-semibold", tone === "success" && "text-success", tone === "warning" && "text-warning")}>{value}</p></div>;
}

function DefaultStat({ icon: Icon, label, value }: { icon: typeof Timer; label: string; value: string }) {
  return <div className="flex items-center gap-2.5 rounded-lg border bg-background/55 p-3"><div className="grid h-8 w-8 place-items-center rounded-lg bg-primary/10 text-primary"><Icon className="h-3.5 w-3.5" /></div><div><p className="text-[10px] text-muted-foreground">{label}</p><p className="mt-0.5 text-xs font-semibold tabular-nums">{value}</p></div></div>;
}

function SideRow({ label, value, mono = false, tone = "default" }: { label: string; value: string; mono?: boolean; tone?: "default" | "success" | "warning" }) {
  return <div className="flex items-center justify-between gap-3 border-b pb-2 last:border-0 last:pb-0"><span className="shrink-0 text-muted-foreground">{label}</span><span className={cn("min-w-0 truncate text-right font-medium", mono && "font-mono text-[10px]", tone === "success" && "text-success", tone === "warning" && "text-warning")}>{value}</span></div>;
}

function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (checked: boolean) => void; label: string }) {
  return <button type="button" role="switch" aria-checked={checked} aria-label={label} onClick={() => onChange(!checked)} className={cn("relative h-6 w-11 shrink-0 rounded-full border transition-colors", checked ? "border-primary bg-primary" : "border-input bg-muted")}><span className={cn("absolute top-[3px] h-[18px] w-[18px] rounded-full bg-white shadow-sm transition-transform", checked ? "translate-x-[21px]" : "translate-x-[3px]")} /></button>;
}
