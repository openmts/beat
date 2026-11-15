import { useDeferredValue, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { PublicHomeRuntime } from "@/features/public/renderer/public-runtime";
import { PublicBlockRenderer } from "@/features/public/renderer/public-renderer";
import { usePublicTheme } from "@/features/public/theme/public-theme";

type StatusFilter = "all" | "online" | "warning" | "offline";

export function PublicHomePage() {
  const { document } = usePublicTheme();
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [status, setStatus] = useState<StatusFilter>("all");
  const [group, setGroup] = useState("all");
  const nodesQuery = useQuery({
    queryKey: ["public-nodes", deferredQuery, status],
    queryFn: () => api.publicNodes(deferredQuery, status),
    refetchInterval: 15_000,
  });
  const groupsQuery = useQuery({ queryKey: ["public-groups"], queryFn: api.publicGroups, refetchInterval: 30_000 });

  const runtime = useMemo<PublicHomeRuntime>(() => {
    const items = nodesQuery.data?.items ?? [];
    const visibleNodes = items.filter((node) => {
      if (group === "all") return true;
      if (group === "ungrouped") return !node.groupId;
      return node.groupId === group;
    });
    return {
      nodes: visibleNodes,
      counts: nodesQuery.data?.counts ?? {},
      groups: groupsQuery.data,
      query,
      setQuery,
      status,
      setStatus,
      group,
      setGroup,
      nodesLoading: nodesQuery.isPending,
      groupsLoading: groupsQuery.isPending,
      error: nodesQuery.isError ? nodesQuery.error.message : null,
      refresh: () => { void nodesQuery.refetch(); void groupsQuery.refetch(); },
    };
  }, [group, groupsQuery.data, groupsQuery.isPending, nodesQuery.data, nodesQuery.error, nodesQuery.isError, nodesQuery.isPending, query, status]);

  return <PublicBlockRenderer page={document.home} runtime={runtime} />;
}
