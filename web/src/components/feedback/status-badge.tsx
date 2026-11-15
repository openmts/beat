import { Badge } from "@/components/ui/badge";
import { statusLabel, statusTone } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { NodeStatus } from "@/api/types";

export function StatusBadge({ status, className }: { status: NodeStatus; className?: string }) {
  const tone = statusTone(status);
  return (
    <Badge variant={tone} className={cn("whitespace-nowrap", className)}>
      <span className={cn("h-1.5 w-1.5 rounded-full", {
        "bg-success": tone === "success",
        "bg-warning": tone === "warning",
        "bg-destructive": tone === "destructive",
        "bg-muted-foreground": tone === "secondary",
      })} />
      {statusLabel(status)}
    </Badge>
  );
}

