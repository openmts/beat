import { LoaderCircle, RefreshCw, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";

export function LoadingState({ label = "正在加载数据…" }: { label?: string }) {
  return (
    <div className="grid min-h-56 place-items-center" role="status" aria-live="polite">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <LoaderCircle className="h-4 w-4 animate-spin" />{label}
      </div>
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="grid min-h-56 place-items-center px-6 py-10">
      <div className="max-w-sm text-center">
        <div className="mx-auto grid h-11 w-11 place-items-center rounded-full bg-destructive/10 text-destructive"><WifiOff className="h-5 w-5" /></div>
        <p className="mt-4 text-sm font-medium">数据加载失败</p>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">{message}</p>
        {onRetry ? <Button className="mt-4" size="sm" variant="outline" onClick={onRetry}><RefreshCw className="h-3.5 w-3.5" />重试</Button> : null}
      </div>
    </div>
  );
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="grid min-h-44 place-items-center px-6 py-10 text-center">
      <div>
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}

