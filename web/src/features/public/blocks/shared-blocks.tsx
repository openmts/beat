import type { PublicThemeBlock } from "@/api/types";
import { cn } from "@/lib/utils";

function propString(block: PublicThemeBlock, key: string, fallback = "") {
  const value = block.props[key];
  return typeof value === "string" ? value : fallback;
}

export function AnnouncementBlock({ block }: { block: PublicThemeBlock }) {
  const title = propString(block, "title", "公告");
  const content = propString(block, "content", "");
  return (
    <div className="rounded-xl border border-primary/15 bg-primary/[0.05] px-4 py-3">
      <p className="text-xs font-semibold text-primary">{title}</p>
      {content ? <p className="mt-1 text-xs leading-5 text-muted-foreground">{content}</p> : null}
    </div>
  );
}

export function SpacerBlock({ block }: { block: PublicThemeBlock }) {
  const size = block.layout.minHeight === "auto" ? "md" : block.layout.minHeight;
  return <div aria-hidden className={cn(size === "sm" && "h-4", size === "md" && "h-8", size === "lg" && "h-14", (size === "tall" || size === "viewport") && "h-24")} />;
}
