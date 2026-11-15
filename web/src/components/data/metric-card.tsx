import type { LucideIcon } from "lucide-react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function MetricCard({ label, value, hint, icon: Icon, tone = "primary", suffix, className }: {
  label: string;
  value: string | number;
  hint?: string;
  icon: LucideIcon;
  tone?: "primary" | "success" | "warning" | "danger" | "neutral" | "violet";
  suffix?: string;
  className?: string;
}) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <div className="flex items-start justify-between gap-3 p-5">
        <div className="min-w-0">
          <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{label}</p>
          <div className="mt-2 flex items-baseline gap-1">
            <p className="text-2xl font-semibold tabular-nums tracking-tight">{value}</p>
            {suffix ? <span className="text-sm font-medium text-muted-foreground">{suffix}</span> : null}
          </div>
          {hint ? <p className="mt-1.5 truncate text-xs text-muted-foreground">{hint}</p> : null}
        </div>
        <div className={cn("rounded-lg p-2.5", {
          "bg-primary/10 text-primary": tone === "primary",
          "bg-success/10 text-success": tone === "success",
          "bg-warning/10 text-warning": tone === "warning",
          "bg-destructive/10 text-destructive": tone === "danger",
          "bg-muted text-muted-foreground": tone === "neutral",
          "bg-chart-network/10 text-chart-network": tone === "violet",
        })}>
          <Icon className="h-[18px] w-[18px]" />
        </div>
      </div>
    </Card>
  );
}

