import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

export function Progress({ value, className, indicatorClassName, ...props }: HTMLAttributes<HTMLDivElement> & { value: number; indicatorClassName?: string }) {
  const safe = Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0));
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-muted", className)} {...props}>
      <div className={cn("h-full rounded-full bg-primary transition-all", indicatorClassName)} style={{ width: `${safe}%` }} />
    </div>
  );
}

