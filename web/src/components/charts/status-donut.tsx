import { cn } from "@/lib/utils";

export function StatusDonut({ online, warning, offline, className }: { online: number; warning: number; offline: number; className?: string }) {
  const total = Math.max(1, online + warning + offline);
  const a = online / total * 100;
  const b = warning / total * 100;
  const background = `conic-gradient(hsl(var(--success)) 0 ${a}%, hsl(var(--warning)) ${a}% ${a + b}%, hsl(var(--destructive)) ${a + b}% 100%)`;
  return (
    <div className={cn("relative grid h-36 w-36 place-items-center rounded-full", className)} style={{ background }}>
      <div className="grid h-[104px] w-[104px] place-items-center rounded-full bg-card text-center shadow-inner">
        <div>
          <p className="text-2xl font-semibold tabular-nums">{online + warning}</p>
          <p className="text-[10px] uppercase tracking-wide text-muted-foreground">活跃节点</p>
        </div>
      </div>
    </div>
  );
}

