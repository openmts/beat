import { useEffect, useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, ArrowRight, KeyRound, LoaderCircle, LockKeyhole, ShieldCheck, UserRound } from "lucide-react";
import { api, ApiError } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

export function LoginPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const auth = useQuery({ queryKey: ["auth", "me"], queryFn: api.authMe });
  const meta = useQuery({ queryKey: ["meta"], queryFn: api.meta });
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const siteName = meta.data?.site.name || "Beat";
  const siteSubtitle = meta.data?.site.subtitle || "NODE OBSERVABILITY";

  useEffect(() => {
    document.title = `登录 · ${siteName}`;
  }, [siteName]);

  useEffect(() => {
    if (auth.data?.authenticated) {
      void navigate({ to: "/admin", replace: true });
    }
  }, [auth.data?.authenticated, navigate]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!username.trim() || !password || pending) return;
    setPending(true);
    setError(null);
    try {
      const result = await api.login(username.trim(), password);
      queryClient.setQueryData(["auth", "me"], result);
      await queryClient.invalidateQueries({ queryKey: ["meta"] });
      await navigate({ to: "/admin", replace: true });
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : "登录失败，请稍后重试");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="relative min-h-screen overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_18%_12%,rgba(37,99,235,0.16),transparent_32%),radial-gradient(circle_at_82%_78%,rgba(14,165,233,0.11),transparent_30%)]" />
      <header className="relative z-10 mx-auto flex h-20 w-full max-w-[1320px] items-center justify-between px-5 sm:px-8">
        <Link to="/" className="flex items-center gap-2.5">
          <div className="relative grid h-9 w-9 place-items-center rounded-xl bg-primary text-primary-foreground shadow-sm shadow-primary/20">
            <Activity className="h-[19px] w-[19px]" strokeWidth={2.4} />
            <span className="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-background bg-success" />
          </div>
          <div>
            <p className="max-w-[190px] truncate text-[15px] font-semibold leading-none tracking-tight">{siteName}</p>
            <p className="mt-1 max-w-[190px] truncate text-[9px] uppercase tracking-[0.2em] text-muted-foreground">{siteSubtitle}</p>
          </div>
        </Link>
        <Link to="/" className="text-xs font-medium text-muted-foreground transition-colors hover:text-foreground">返回节点广场</Link>
      </header>

      <main className="relative z-10 mx-auto grid min-h-[calc(100vh-5rem)] w-full max-w-[1320px] items-center gap-10 px-5 pb-12 sm:px-8 lg:grid-cols-[minmax(0,1fr)_440px]">
        <section className="hidden max-w-xl lg:block">
          <Badge variant="outline" className="border-primary/20 bg-card/70 px-3 py-1 text-primary"><ShieldCheck className="h-3.5 w-3.5" />单账号安全访问</Badge>
          <h1 className="mt-6 text-[42px] font-semibold leading-[1.12] tracking-[-0.035em]">让每一次节点观测<br /><span className="text-primary">都有据可查。</span></h1>
          <p className="mt-5 max-w-lg text-sm leading-7 text-muted-foreground">登录 Beat 管理后台，统一管理节点、分组与 Agent 接入。前台节点广场继续对外公开，后台配置与写操作仅限授权账号访问。</p>
          <div className="mt-9 grid max-w-lg grid-cols-3 gap-3">
            {[
              ["多值模型", "MTS 时序存储"],
              ["gRPC 双向流", "Agent 轻量采集"],
              ["公开视图", "前台无需登录"],
            ].map(([title, description]) => (
              <div key={title} className="rounded-xl border bg-card/65 p-3.5 shadow-sm backdrop-blur-sm">
                <p className="text-xs font-semibold">{title}</p>
                <p className="mt-1.5 text-[10px] leading-5 text-muted-foreground">{description}</p>
              </div>
            ))}
          </div>
        </section>

        <Card className="mx-auto w-full max-w-md overflow-hidden border-white/50 bg-card/95 shadow-float backdrop-blur-xl dark:border-white/10">
          <div className="h-1 bg-gradient-to-r from-primary via-sky-400 to-cyan-300" />
          <CardContent className="p-6 sm:p-8">
            <div className="grid h-11 w-11 place-items-center rounded-xl bg-primary/10 text-primary"><LockKeyhole className="h-5 w-5" /></div>
            <h2 className="mt-5 text-xl font-semibold tracking-tight">登录管理后台</h2>
            <p className="mt-1.5 text-xs leading-5 text-muted-foreground">使用 Beat 管理员账号继续。登录会话默认保持 12 小时。</p>

            <form className="mt-6 space-y-4" onSubmit={submit}>
              <label className="block space-y-1.5">
                <span className="text-xs font-medium text-muted-foreground">登录账号</span>
                <div className="relative">
                  <UserRound className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" autoFocus maxLength={64} className="h-11 pl-9" placeholder="admin" />
                </div>
              </label>
              <label className="block space-y-1.5">
                <span className="text-xs font-medium text-muted-foreground">密码</span>
                <div className="relative">
                  <KeyRound className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" className="h-11 pl-9" placeholder="请输入密码" />
                </div>
              </label>

              {error ? <div role="alert" className="rounded-lg border border-destructive/20 bg-destructive/[0.06] px-3 py-2.5 text-xs text-destructive">{error}</div> : null}

              <Button type="submit" className="h-11 w-full" disabled={pending || !username.trim() || !password}>
                {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                {pending ? "正在验证…" : "进入管理后台"}
                {!pending ? <ArrowRight className="ml-auto h-4 w-4" /> : null}
              </Button>
            </form>

            <div className="mt-5 rounded-xl border bg-muted/35 p-3.5">
              <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">首次登录</p>
              <p className="mt-1.5 text-[11px] leading-5 text-muted-foreground">默认账号与密码均为 <code className="rounded border bg-card px-1.5 py-0.5 font-mono text-[10px] text-foreground">admin</code>。登录后请立即前往系统设置修改凭据。</p>
            </div>
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
