import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import type { PublicDetailRuntime } from "@/features/public/renderer/public-runtime";
import { PublicBlockRenderer } from "@/features/public/renderer/public-renderer";
import { usePublicTheme } from "@/features/public/theme/public-theme";

type Range = "1h" | "6h" | "24h" | "7d";

export function PublicNodeDetailPage({ nodeId }: { nodeId: string }) {
  const { document } = usePublicTheme();
  const configuredRange = document.nodeDetail.blocks.find((block) => block.type === "metric-trends")?.props.defaultRange;
  const initialRange: Range = configuredRange === "1h" || configuredRange === "6h" || configuredRange === "7d" ? configuredRange : "24h";
  const [range, setRange] = useState<Range>(initialRange);
  const nodeQuery = useQuery({ queryKey: ["public-node", nodeId], queryFn: () => api.publicNode(nodeId), refetchInterval: 15_000 });
  const metricsQuery = useQuery({
    queryKey: ["public-node-metrics", nodeId, range],
    queryFn: () => api.publicNodeMetrics(nodeId, range),
    refetchInterval: 30_000,
    enabled: nodeQuery.isSuccess,
  });
  const probesQuery = useQuery({
    queryKey: ["public-node-probes", nodeId, range],
    queryFn: () => api.publicNodeNetworkProbes(nodeId, range),
    refetchInterval: 30_000,
    enabled: nodeQuery.isSuccess,
  });

  const runtime = useMemo<PublicDetailRuntime | null>(() => {
    if (!nodeQuery.data) return null;
    return {
      nodeId,
      node: nodeQuery.data,
      metrics: metricsQuery.data,
      probes: probesQuery.data,
      range,
      setRange,
      metricsLoading: metricsQuery.isPending,
      metricsError: metricsQuery.isError ? metricsQuery.error.message : null,
      refresh: () => { void nodeQuery.refetch(); void metricsQuery.refetch(); void probesQuery.refetch(); },
    };
  }, [metricsQuery.data, metricsQuery.error, metricsQuery.isError, metricsQuery.isPending, nodeId, nodeQuery.data, probesQuery.data, range]);

  if (nodeQuery.isPending) return <LoadingState label="正在读取节点详情…" />;
  if (nodeQuery.isError) return <ErrorState message={nodeQuery.error.message} onRetry={() => void nodeQuery.refetch()} />;
  if (!runtime) return <LoadingState label="正在准备主题模块…" />;
  return <PublicBlockRenderer page={document.nodeDetail} runtime={runtime} />;
}
