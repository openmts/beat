import type { NodeMetricsResponse, NodeNetworkProbesResponse, PublicGroupsResponse, PublicNode } from "@/api/types";

export type PublicHomeRuntime = {
  nodes: PublicNode[];
  counts: Record<string, number>;
  groups: PublicGroupsResponse | undefined;
  query: string;
  setQuery: (value: string) => void;
  status: "all" | "online" | "warning" | "offline";
  setStatus: (value: "all" | "online" | "warning" | "offline") => void;
  group: string;
  setGroup: (value: string) => void;
  nodesLoading: boolean;
  groupsLoading: boolean;
  error: string | null;
  refresh: () => void;
};

export type PublicDetailRuntime = {
  nodeId: string;
  node: PublicNode;
  metrics: NodeMetricsResponse | undefined;
  probes: NodeNetworkProbesResponse | undefined;
  range: "1h" | "6h" | "24h" | "7d";
  setRange: (value: "1h" | "6h" | "24h" | "7d") => void;
  metricsLoading: boolean;
  metricsError: string | null;
  refresh: () => void;
};

export type PublicRuntime = PublicHomeRuntime | PublicDetailRuntime;

export function publicGridClass(columns: number): string {
  if (columns === 2) return "public-grid-2 grid gap-3 sm:grid-cols-2";
  if (columns === 3) return "public-grid-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-3";
  if (columns === 4) return "public-grid-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4";
  if (columns === 1) return "grid gap-3";
  return "grid gap-3";
}
