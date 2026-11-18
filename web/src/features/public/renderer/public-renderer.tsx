import { Component, type ComponentType, type CSSProperties, type ErrorInfo, type ReactNode } from "react";
import type { PublicThemeBlock, PublicThemePage } from "@/api/types";
import { NodeCollectionBlock } from "@/features/public/blocks/home-blocks";
import { GroupFilterBlock, HeroBlock, HomeSummaryBlock } from "@/features/public/blocks/home-blocks";
import { PaperMonitorBlock } from "@/features/public/blocks/paper-monitor";
import { MetricSummaryBlock, MetricTrendsBlock, NetworkProbesBlock, NodeHeaderBlock, SystemInfoBlock } from "@/features/public/blocks/detail-blocks";
import { AnnouncementBlock, SpacerBlock } from "@/features/public/blocks/shared-blocks";
import type { PublicRuntime } from "@/features/public/renderer/public-runtime";
import { cn } from "@/lib/utils";

type BlockProps = { block: PublicThemeBlock; runtime: PublicRuntime };

const COMPONENTS: Record<string, ComponentType<BlockProps>> = {
  hero: HeroBlock as ComponentType<BlockProps>,
  "summary-stats": HomeSummaryBlock as ComponentType<BlockProps>,
  "group-filter": GroupFilterBlock as ComponentType<BlockProps>,
  "node-collection": NodeCollectionBlock as ComponentType<BlockProps>,
  "paper-monitor": PaperMonitorBlock as ComponentType<BlockProps>,
  "node-header": NodeHeaderBlock as ComponentType<BlockProps>,
  "system-info": SystemInfoBlock as ComponentType<BlockProps>,
  "metric-summary": MetricSummaryBlock as ComponentType<BlockProps>,
  "metric-trends": MetricTrendsBlock as ComponentType<BlockProps>,
  "network-probes": NetworkProbesBlock as ComponentType<BlockProps>,
  announcement: AnnouncementBlock,
  spacer: SpacerBlock,
};

export function PublicBlockRenderer({ page, runtime }: { page: PublicThemePage; runtime: PublicRuntime }) {
  return (
    <div className="public-stack flex flex-col">
      {page.blocks.filter((block) => block.enabled).map((block) => (
        <PublicBlockErrorBoundary key={block.id}>
          <PublicBlockFrame block={block}>
            <BlockBody block={block} runtime={runtime} />
          </PublicBlockFrame>
        </PublicBlockErrorBoundary>
      ))}
    </div>
  );
}

function BlockBody({ block, runtime }: { block: PublicThemeBlock; runtime: PublicRuntime }) {
  const RuntimeBlock = COMPONENTS[block.type] as ComponentType<BlockProps> | undefined;
  if (!RuntimeBlock) return null;
  return <RuntimeBlock block={block} runtime={runtime} />;
}

function PublicBlockFrame({ block, children }: { block: PublicThemeBlock; children: ReactNode }) {
  return (
    <section
      data-block-id={block.id}
      data-block-type={block.type}
      className={cn("min-w-0", paddingClass(block.layout.padding))}
      style={minHeightStyle(block.layout.minHeight)}
    >
      {children}
    </section>
  );
}

function paddingClass(padding: PublicThemeBlock["layout"]["padding"]) {
  if (padding === "sm") return "p-3";
  if (padding === "md") return "p-4";
  if (padding === "lg") return "p-5 sm:p-7";
  return "";
}

function minHeightStyle(height: PublicThemeBlock["layout"]["minHeight"]): CSSProperties | undefined {
  if (height === "auto") return undefined;
  if (height === "viewport") return { minHeight: "calc(100dvh - 10rem)" };
  const values = { sm: "6rem", md: "12rem", lg: "20rem", tall: "28rem" } as const;
  return { minHeight: values[height] };
}

class PublicBlockErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("public block render failed", error, info.componentStack);
  }

  render() {
    if (this.state.failed) return null;
    return this.props.children;
  }
}
