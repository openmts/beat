import { useEffect, useState, type MouseEvent as ReactMouseEvent } from "react";
import { createPortal } from "react-dom";
import { ArrowUpRight, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Progress } from "@/components/ui/progress";
import { StatusBadge } from "@/components/feedback/status-badge";
import { formatPercent, formatRate, formatRelative } from "@/lib/format";
import { groupColor } from "@/lib/groups";
import { cn } from "@/lib/utils";
import type { Node } from "@/api/types";

export function NodeTable({
  nodes,
  compact = false,
  admin = false,
  className,
  onEdit,
  onUninstall,
}: {
  nodes: Node[];
  compact?: boolean;
  admin?: boolean;
  className?: string;
  onEdit?: (node: Node) => void;
  onUninstall?: (node: Node) => void;
}) {
  const hasActions = Boolean(onEdit || onUninstall);
  const [menu, setMenu] = useState<{ node: Node; top: number; left: number } | null>(null);

  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(null);
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === "Escape") close(); };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [menu]);

  function openMenu(event: ReactMouseEvent<HTMLButtonElement>, node: Node) {
    const rect = event.currentTarget.getBoundingClientRect();
    const width = 184;
    const height = 104;
    const left = Math.max(12, Math.min(rect.right - width, window.innerWidth - width - 12));
    const top = window.innerHeight - rect.bottom >= height + 8 ? rect.bottom + 6 : rect.top - height - 6;
    setMenu({ node, left, top: Math.max(12, top) });
  }

  return (
    <>
    <Table className={cn("min-w-[820px]", hasActions && "min-w-[860px]", className)}>
      <TableHeader>
        <TableRow>
          <TableHead>节点</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="w-36">CPU</TableHead>
          <TableHead className="w-36">内存</TableHead>
          <TableHead className="w-32">磁盘</TableHead>
          {compact ? null : <TableHead>网络</TableHead>}
          <TableHead>最后上报</TableHead>
          {hasActions ? <TableHead className="w-14 text-center">操作</TableHead> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {nodes.map((node) => (
          <TableRow key={node.id} className="group">
            <TableCell>
              <NodeLink node={node} admin={admin} />
            </TableCell>
            <TableCell><StatusBadge status={node.status} /></TableCell>
            <TableCell>
              <MetricBar value={node.metrics.cpuUsage} />
            </TableCell>
            <TableCell>
              <MetricBar value={node.metrics.memoryUsage} />
            </TableCell>
            <TableCell>
              <MetricBar value={node.metrics.diskUsage} warningAt={85} />
            </TableCell>
            {compact ? null : (
              <TableCell>
                <p className="text-xs tabular-nums">↓ {formatRate(node.metrics.networkRxBps)}</p>
                <p className="text-xs tabular-nums text-muted-foreground">↑ {formatRate(node.metrics.networkTxBps)}</p>
              </TableCell>
            )}
            <TableCell className="whitespace-nowrap text-xs text-muted-foreground">{formatRelative(node.lastSeen)}</TableCell>
            {hasActions ? (
              <TableCell className="text-center">
                <button
                  type="button"
                  aria-label={`打开 ${node.hostname} 操作菜单`}
                  aria-haspopup="menu"
                  aria-expanded={menu?.node.id === node.id}
                  onClick={(event) => menu?.node.id === node.id ? setMenu(null) : openMenu(event, node)}
                  className={cn("inline-grid h-8 w-8 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground", menu?.node.id === node.id && "bg-muted text-foreground")}
                >
                  <MoreHorizontal className="h-4 w-4" />
                </button>
              </TableCell>
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
      {menu ? createPortal(
        <>
          <div className="fixed inset-0 z-40" onMouseDown={() => setMenu(null)} />
          <div role="menu" aria-label={`${menu.node.hostname} 操作菜单`} className="fixed z-50 w-[184px] overflow-hidden rounded-lg border bg-card p-1 shadow-xl" style={{ top: menu.top, left: menu.left }}>
            {onEdit ? (
              <button type="button" role="menuitem" autoFocus onClick={() => { const node = menu.node; setMenu(null); onEdit(node); }} className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium text-foreground transition-colors hover:bg-primary/10 hover:text-primary">
                <Pencil className="h-3.5 w-3.5" />编辑节点配置
              </button>
            ) : null}
            {onEdit && onUninstall ? <div className="my-1 h-px bg-border" /> : null}
            {onUninstall ? (
              <button type="button" role="menuitem" onClick={() => { const node = menu.node; setMenu(null); onUninstall(node); }} className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium text-destructive transition-colors hover:bg-destructive/10">
                <Trash2 className="h-3.5 w-3.5" />卸载 Agent
              </button>
            ) : null}
          </div>
        </>,
        document.body,
      ) : null}
    </>
  );
}

function NodeLink({ node, admin }: { node: Node; admin: boolean }) {
  const group = node.groupId ? groupColor(node.groupColor) : null;
  const content = (
    <>
      <span className="flex items-center gap-2 font-medium text-foreground group-hover:text-primary">
        {node.hostname}<ArrowUpRight className="h-3 w-3 opacity-0 transition-opacity group-hover:opacity-100" />
      </span>
      <span className="mt-0.5 block font-mono text-[10px] text-muted-foreground">{node.address || node.agentId}</span>
      <span className={cn("mt-1 inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[9px]", group?.chip ?? "border-dashed bg-muted/30 text-muted-foreground")}>
        <span className={cn("h-1 w-1 rounded-full", group?.dot ?? "bg-muted-foreground/50")} />
        {group ? node.groupName : "未分组"}
      </span>
    </>
  );
  if (admin) {
    return <Link to="/admin/nodes/$nodeId" params={{ nodeId: node.id }} className="block min-w-0">{content}</Link>;
  }
  return <Link to="/nodes/$nodeId" params={{ nodeId: node.id }} className="block min-w-0">{content}</Link>;
}

function MetricBar({ value, warningAt = 90 }: { value: number; warningAt?: number }) {
  return (
    <div className="max-w-[116px]">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="text-xs font-medium tabular-nums">{formatPercent(value)}</span>
      </div>
      <Progress value={value} indicatorClassName={value >= warningAt ? "bg-warning" : value >= 70 ? "bg-primary" : "bg-success"} />
    </div>
  );
}

