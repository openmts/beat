import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Check,
  Clipboard,
  Clock3,
  Download,
  ExternalLink,
  KeyRound,
  Laptop,
  LoaderCircle,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Terminal,
} from "lucide-react";
import type { AgentArchitecture, AgentEnrollment } from "@/api/types";
import { api } from "@/api/client";
import { AgentUpgradeCard } from "@/features/agents/agent-upgrade-card";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";

export function AgentsPage() {
  const agents = useQuery({ queryKey: ["agents"], queryFn: api.agents, refetchInterval: 15_000 });
  const [copied, setCopied] = useState<string | null>(null);
  const [name, setName] = useState("edge-node-03");
  const [labels, setLabels] = useState("region=cn-east,role=edge");
  const [architecture, setArchitecture] = useState<AgentArchitecture["id"]>("amd64");
  const [enrollment, setEnrollment] = useState<AgentEnrollment | null>(null);
  const [now, setNow] = useState(Date.now());
  const enrollmentMutation = useMutation({
    mutationFn: () => api.createAgentToken(name, labels),
    onSuccess: (data) => {
      setEnrollment(data);
      setNow(Date.now());
    },
  });

  useEffect(() => {
    if (!enrollment) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, [enrollment]);

  if (agents.isPending) return <LoadingState label="正在读取代理状态…" />;
  if (agents.isError) return <ErrorState message={agents.error.message} onRetry={() => void agents.refetch()} />;
  const selectedArchitecture = agents.data.architectures.find((item) => item.id === architecture) ?? agents.data.architectures[0];
  const remainingSeconds = enrollment ? Math.max(0, Math.ceil((new Date(enrollment.expiresAt).getTime() - now) / 1000)) : 0;
  const expired = Boolean(enrollment) && remainingSeconds === 0;
  if (!selectedArchitecture) return <ErrorState message="Agent 架构信息不可用" onRetry={() => void agents.refetch()} />;

  async function copy(text: string, key: string) {
    try {
      await copyText(text);
      setCopied(key);
      window.setTimeout(() => setCopied(null), 1_800);
    } catch {
      setCopied(null);
    }
  }

  return (
    <div className="min-w-0 space-y-5 animate-fade-in">
      <PageHeader
        eyebrow="Node onboarding"
        title="节点接入"
        description="从 GitHub Release 下载 Agent、生成短期注册令牌并复制完整接入命令。注册令牌默认 10 分钟有效，每次生成都会轮换。"
        actions={<>
          <Button variant="outline" size="sm" onClick={() => void agents.refetch()}><RefreshCw className={cn("h-3.5 w-3.5", agents.isFetching && "animate-spin")} />刷新配置</Button>
          <a href={selectedArchitecture.downloadUrl} download="beat-agent" className={cn(buttonVariants({ size: "sm" }), !selectedArchitecture.available && "pointer-events-none opacity-50")} aria-disabled={!selectedArchitecture.available}>
            <Download className="h-3.5 w-3.5" />下载 Agent
          </a>
        </>}
      />

      <AgentUpgradeCard />

      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1.55fr)_minmax(320px,1fr)]">
        <Card className="min-w-0 overflow-hidden">
          <div className="border-b bg-gradient-to-r from-slate-950 via-slate-900 to-slate-800 p-5 text-white">
            <div className="flex min-w-0 items-start justify-between gap-4">
              <div className="min-w-0">
                <Badge className="border-white/10 bg-white/10 text-sky-200">Quick start</Badge>
                <h2 className="mt-3 text-lg font-semibold tracking-tight">60 秒接入新节点</h2>
                <p className="mt-1 max-w-xl text-xs leading-5 text-slate-300">请先按目标架构执行右侧的一键安装命令，再生成接入命令。接入命令会写入 0600 权限的 agent.yaml、重新加载 systemd 并启动 Agent。</p>
              </div>
              <div className="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-white/10"><Terminal className="h-5 w-5 text-sky-300" /></div>
            </div>
          </div>

          <CardContent className="min-w-0 space-y-4 p-4 sm:p-5">
            <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_auto] lg:items-end">
              <label className="min-w-0 space-y-1.5 text-[11px] font-medium text-muted-foreground">节点名称<Input value={name} onChange={(event) => setName(event.target.value)} placeholder="edge-node-03" /></label>
              <label className="min-w-0 space-y-1.5 text-[11px] font-medium text-muted-foreground">节点标签<Input value={labels} onChange={(event) => setLabels(event.target.value)} placeholder="region=cn-east,role=edge" /></label>
              <Button type="button" size="default" onClick={() => enrollmentMutation.mutate()} disabled={enrollmentMutation.isPending}>
                {enrollmentMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
                生成接入命令
              </Button>
            </div>

            {enrollmentMutation.isError ? <div className="rounded-lg border border-destructive/20 bg-destructive/[0.05] px-3 py-2 text-[11px] text-destructive">{enrollmentMutation.error.message}</div> : null}

            {enrollment ? (
              <div className="space-y-3">
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <Badge variant={expired ? "warning" : "success"}><Clock3 className="mr-1 h-3 w-3" />{expired ? "注册窗口已关闭" : `注册窗口 ${formatCountdown(remainingSeconds)}`}</Badge>
                  <span className="min-w-0 break-words text-[10px] text-muted-foreground">过期时间 {formatTime(enrollment.expiresAt)} · 首次上报后绑定当前节点，已绑定 Agent 不受关闭影响</span>
                </div>
                <div className="relative min-w-0 overflow-hidden rounded-lg bg-[#0b1526] px-4 py-4 text-slate-200">
                  <pre className="max-h-44 min-w-0 overflow-auto whitespace-pre-wrap break-all pr-20 font-mono text-[11px] leading-6 scrollbar-thin">{enrollment.accessCommand}</pre>
                  <Button size="sm" variant="secondary" className="absolute right-3 top-3 h-7 border-white/10 bg-white/10 text-white hover:bg-white/15" onClick={() => void copy(enrollment.accessCommand, "command")}>
                    {copied === "command" ? <Check className="h-3 w-3" /> : <Clipboard className="h-3 w-3" />}{copied === "command" ? "已复制" : "复制命令"}
                  </Button>
                </div>
                <div className="flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground">
                  <span className="font-mono">令牌 …{enrollment.token.slice(-10)}</span>
                  <span>·</span>
                  <button type="button" className="inline-flex items-center gap-1 text-primary hover:underline" onClick={() => void copy(enrollment.token, "token")}>{copied === "token" ? <Check className="h-3 w-3" /> : <Clipboard className="h-3 w-3" />}复制令牌</button>
                  <span>·</span>
                  <span>每次点击生成都会创建全新令牌</span>
                </div>
              </div>
            ) : (
              <div className="grid min-h-36 place-items-center rounded-lg border border-dashed bg-muted/20 text-center">
                <div className="px-4"><KeyRound className="mx-auto h-7 w-7 text-muted-foreground/70" /><p className="mt-2 text-xs font-medium">尚未生成接入命令</p><p className="mt-1 text-[10px] text-muted-foreground">填写节点名称和标签后，点击“生成接入命令”。</p></div>
              </div>
            )}
          </CardContent>
        </Card>

        <div className="grid min-w-0 gap-4">
          <Card className="min-w-0">
            <CardHeader><CardTitle className="flex items-center gap-2"><Download className="h-4 w-4 text-primary" />Agent 一键安装</CardTitle><p className="text-xs text-muted-foreground">选择目标节点架构，从 GitHub Release 下载并自动完成目录、二进制和 systemd 初始化。</p></CardHeader>
            <CardContent className="min-w-0 space-y-3">
              <div className="grid min-w-0 grid-cols-2 gap-1 rounded-lg border bg-muted/30 p-1" role="group" aria-label="Agent 目标架构">
                {agents.data.architectures.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    aria-pressed={item.id === selectedArchitecture.id}
                    onClick={() => setArchitecture(item.id)}
                    className={cn(
                      "min-w-0 rounded-md px-1.5 py-1.5 text-[10px] font-medium leading-4 transition-colors",
                      item.id === selectedArchitecture.id ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-lg border bg-background/55 px-3 py-2.5 text-[11px]"><span className="flex min-w-0 items-center gap-2"><Laptop className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /><span className="truncate">linux / {selectedArchitecture.id}</span>{selectedArchitecture.version ? <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{selectedArchitecture.version}</span> : null}</span><Badge variant={selectedArchitecture.available ? "success" : "warning"}>{selectedArchitecture.available ? "可下载" : "未就绪"}</Badge></div>
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-lg border border-primary/15 bg-primary/[0.04] px-3 py-2 text-[10px] leading-5 text-muted-foreground"><span className="min-w-0 break-words">发布源：<span className="font-mono text-foreground">{agents.data.releaseRepository}</span></span><a href={agents.data.releaseUrl} target="_blank" rel="noreferrer" className="inline-flex shrink-0 items-center gap-1 font-medium text-primary hover:underline">GitHub Release <ExternalLink className="h-3 w-3" /></a></div>
              {!selectedArchitecture.available ? <div className="rounded-lg border border-warning/30 bg-warning/[0.06] px-3 py-2 text-[10px] leading-5 text-warning">{selectedArchitecture.label} 的 GitHub Release 资产尚不可用，请检查发布流水线或当前 release tag。</div> : null}
              <div className="relative min-w-0 rounded-lg bg-muted/45 p-3 pr-10 font-mono text-[10px] leading-5 text-muted-foreground">
                <pre className="max-h-64 min-w-0 overflow-auto whitespace-pre-wrap break-all pr-5 font-mono scrollbar-thin">{selectedArchitecture.downloadCommand}</pre>
                <button type="button" aria-label="复制下载命令" className="absolute right-2.5 top-2.5 rounded p-1 hover:bg-background hover:text-foreground" onClick={() => void copy(selectedArchitecture.downloadCommand, "download")}>{copied === "download" ? <Check className="h-3.5 w-3.5 text-success" /> : <Clipboard className="h-3.5 w-3.5" />}</button>
              </div>
              <p className="text-[10px] leading-5 text-muted-foreground">命令会从 GitHub Release 下载对应架构的二进制，校验主机架构与 SHA-256 后直接安装为 <code className="font-mono text-foreground">/usr/local/bin/beat-agent</code>；配置写入 <code className="font-mono text-foreground">/etc/beat/agent.yaml</code> 并生成 root 运行的 systemd 单元。生成接入命令后才会启用并启动服务。</p>
              {selectedArchitecture.checksumSha256 ? <p className="break-all rounded-lg border border-success/20 bg-success/[0.04] px-3 py-2 font-mono text-[9px] leading-4 text-muted-foreground"><span className="font-sans font-medium text-success">SHA-256</span><br />{selectedArchitecture.checksumSha256}</p> : null}
            </CardContent>
          </Card>

          <Card className="min-w-0">
            <CardHeader><CardTitle className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-primary" />令牌策略</CardTitle><p className="text-xs text-muted-foreground">短期注册令牌，降低长期凭据泄漏风险。</p></CardHeader>
            <CardContent className="min-w-0 space-y-3 text-xs">
              <PolicyRow label="默认有效期" value={`${Math.round(agents.data.tokenTtlSeconds / 60)} 分钟`} />
              <PolicyRow label="令牌生成" value="每次随机生成" />
              <PolicyRow label="使用范围" value="首次上报绑定节点" />
              <PolicyRow label="已绑定 Agent" value="持续上报不受影响" />
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function PolicyRow({ label, value }: { label: string; value: string }) {
  return <div className="flex min-w-0 items-center justify-between gap-3 border-b pb-2.5 last:border-0 last:pb-0"><span className="shrink-0 text-muted-foreground">{label}</span><span className="min-w-0 break-words text-right font-medium">{value}</span></div>;
}

function formatCountdown(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(remainder).padStart(2, "0")}`;
}

async function copyText(text: string) {
  if (window.isSecureContext && navigator.clipboard) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("copy failed");
}
